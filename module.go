package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strings"
)

type module struct {
	File     string
	Name     string         `json:"name"`
	Request  moduleRequest  `json:"request"`
	Response moduleResponse `json:"response"`
}

type moduleRequest struct {
	Method  string   `json:"method"`
	Path    string   `json:"path"`
	Paths   []string `json:"paths"`
	Body    string   `json:"body"`
	Headers []string `json:"headers"`
}

type moduleResponse struct {
	Matches         []moduleMatch `json:"matches"`
	MustNotMatch    []moduleMatch `json:"mustNotMatch"`
	MatchesRequired int           `json:"matchesRequired"`
}

type moduleMatch struct {
	Type     string `json:"type"`
	Pattern  string `json:"pattern"`
	Required bool   `json:"required"`
	Code     int    `json:"code"`
	Name     string `json:"name"`
}

type finding struct {
	Hit bool
	Msg string
}

func newModule(filename string) (*module, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	m := &module{}
	d := json.NewDecoder(f)
	err = d.Decode(m)
	if err != nil {
		return nil, err
	}

	m.File = filename
	if m.Name == "" {
		m.Name = m.File
	}

	if m.Request.Path == "" && len(m.Request.Paths) == 0 {
		return nil, errors.New("module must specify request.path, or request.paths")
	}

	if len(m.Response.Matches) == 0 {
		return nil, errors.New("module must have at least one match")
	}

	// default to one match required
	if m.Response.MatchesRequired == 0 {
		m.Response.MatchesRequired = 1
	}

	return m, nil
}

func (m *module) newRequest(baseURL, path string) (*http.Request, error) {
	req, err := http.NewRequest(
		m.Request.Method,
		baseURL+path,
		bytes.NewBuffer([]byte(m.Request.Body)),
	)
	if err != nil {
		return nil, err
	}

	for _, h := range m.Request.Headers {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) != 2 {
			continue
		}

		// There's a small chance that we actually want to preserve
		// the space on the header key or value, but we'll deal with
		// that issue if it ever comes up.
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		req.Header.Set(k, v)
	}

	return req, nil
}

func (m *module) run(baseURL string, client *http.Client) ([]*finding, error) {

	out := make([]*finding, 0)

	// Default to using the paths list, fall back to single path
	paths := m.Request.Paths
	if len(paths) == 0 {
		paths = []string{m.Request.Path}
	}

	for _, path := range paths {
		req, err := m.newRequest(baseURL, path)
		if err != nil {
			return nil, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		resp.Body.Close()

		// TODO: a way to save responses; defaulting to responses that matched,
		// but with an option to save all responses for debugging etc

		f, err := m.checkMatches(resp, string(body), baseURL)
		if err != nil {
			// stop on any error - might come back to bite us, but
			// seems like the sensible way to handle things for now
			return out, err
		}

		// add to the list of findings
		out = append(out, f)
	}
	return out, nil
}

func (m *module) checkMatches(resp *http.Response, body, baseURL string) (*finding, error) {
	// Track the number of matches we get
	count := 0

	// There's a few situations (e.g. required matches) where we might
	// need to enforce that there's no hit regardless of anything else
	mustNotHit := false

	// The thing that does the actual checking.
	// we're going to use this a couple of times for positive and negative
	// matches so it's worth pulling it out into a function
	test := func(match moduleMatch, body string) (bool, error) {
		switch match.Type {
		case "static":
			return strings.Contains(body, match.Pattern), nil

		case "regex":
			re := regexp.MustCompile(match.Pattern)
			return re.MatchString(body), nil

		case "status":
			// TODO: support mutliple status codes
			return resp.StatusCode == match.Code, nil

		case "header":
			// TODO: deal with multiple response headers
			val := resp.Header.Get(match.Name)
			re := regexp.MustCompile(match.Pattern)
			return re.MatchString(val), nil

		default:
			return false, fmt.Errorf("unknown match type '%s'", match.Type)
		}
	}

	// Positive matches
	for _, match := range m.Response.Matches {
		matched, err := test(match, body)
		if err != nil {
			return nil, err
		}

		if !matched && match.Required {
			mustNotHit = true
		}

		if matched {
			count++
		}
	}

	// Negative matches
	for _, match := range m.Response.MustNotMatch {
		matched, err := test(match, body)
		if err != nil {
			return nil, err
		}

		if matched {
			mustNotHit = true
		}
	}

	// default to counting the number of matches, but then override
	// the result if we saw any reason that we must not hit
	hit := count >= m.Response.MatchesRequired
	if mustNotHit {
		hit = false
	}

	f := &finding{
		Hit: hit,
		Msg: fmt.Sprintf("%s at %s (%d matches)", m.Name, resp.Request.URL.String(), count),
	}

	return f, nil
}
