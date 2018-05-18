package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Controller manages login and logout of an MD
type Controller struct {
	client   http.Client
	url      string
	username string
	password string
	md       string
	reg      *regexp.Regexp
}

// Session encapsulates a session to the controller
type Session struct {
	controller *Controller
	token      string
}

type loginResponse struct {
	GlobalResult struct {
		Status    string `json:"status"`
		StatusStr string `json:"status_str"`
		UIDARUBA  string `json:"UIDARUBA"`
	} `json:"_global_result"`
}

// NewController opens a session to a controller
func NewController(md, username, pass string, timeout time.Duration, skipVerify bool) *Controller {
	// Non-alphanumeric characters will get replaced by "_" in names
	reg, _ := regexp.Compile("[^a-zA-Z0-9]+")
	return &Controller{
		client: http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: skipVerify},
			},
		},
		url:      fmt.Sprintf("https://%s:4343/v1", md),
		reg:      reg,
		username: username,
		password: pass,
		md:       md,
	}
}

// Session creates a new session to the controller
func (c *Controller) Session() (*Session, error) {
	apiURL, data := c.url+"/api/login", url.Values{}
	data.Set("username", c.username)
	data.Set("password", c.password)
	req, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Accept", "application/json")
	resp, err := c.client.Do(req)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("MD %s: Login incorrect (username %s)", c.md, c.username)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("MD %s: Could not read login response", c.md)
	}
	lr := loginResponse{}
	if err := json.Unmarshal(body, &lr); err != nil {
		return nil, fmt.Errorf("MD %s: Expected login response, got %s", c.md, string(body))
	}
	return &Session{controller: c, token: lr.GlobalResult.UIDARUBA}, nil
}

// Close the session.
func (s *Session) Close() error {
	apiURL := fmt.Sprintf("%s/api/logout?UIDARUBA=%s", s.controller.url, s.token)
	resp, err := s.controller.client.Get(apiURL)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("Logout returned error code %d (%s)", resp.StatusCode, resp.Status)
	}
	return nil
}

// Show runs a command on the controller, filters the output through the jsonpath expression, and gets the requested attribs
func (s *Session) Show(cmd string, path Lookup) (interface{}, error) {
	apiURL := fmt.Sprintf("%s/configuration/showcommand?command=%s&json=1&UIDARUBA=%s",
		s.controller.url, url.QueryEscape(cmd), s.token)
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Cookie", fmt.Sprintf("SESSION=%s", s.token))
	resp, err := s.controller.client.Do(req)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Run '%s' returned error code %d (%s)", cmd, resp.StatusCode, resp.Status)
	}
	dec := json.NewDecoder(resp.Body)
	var data interface{}
	if err := dec.Decode(&data); err != nil {
		return nil, err
	}
	data = noWhitespace(data, s.controller.reg)
	if path != nil {
		lookup, err := path.Lookup(data)
		if err != nil {
			return nil, err
		}
		data = lookup
	}
	return data, nil
}

// Switches lists the IP addresses of the switches that comply with the given jsonpath filters
// e.g. Switches("?(@.State=='up')") return switches up
func (c *Controller) Switches(filter Lookup) ([]string, error) {
	// Prefilter, always on:
	path, err := NewLookup("$.All_Switches[?(@.Status == 'up')]")
	if err != nil {
		return nil, err
	}
	if filter != nil {
		path = append(path, filter)
	}
	session, err := c.Session()
	if err != nil {
		return nil, err
	}
	defer session.Close()
	data, err := session.Show("show switches", path)
	if err != nil {
		return nil, err
	}
	return Select(data, []string{"IP_Address"})
}

// noWhitespace removes non-alphanumeric characters from keys
func noWhitespace(data interface{}, reg *regexp.Regexp) interface{} {
	switch data := data.(type) {
	case map[string]interface{}:
		norm := make(map[string]interface{})
		for k, v := range data {
			k = reg.ReplaceAllString(k, "_")
			// Remove also heading and trailing underscores
			for strings.HasPrefix(k, "_") {
				k = k[1:]
			}
			for strings.HasSuffix(k, "_") {
				k = k[:len(k)-1]
			}
			norm[k] = noWhitespace(v, reg)
		}
		return norm
	case []interface{}:
		norm := make([]interface{}, 0, len(data))
		for _, v := range data {
			norm = append(norm, noWhitespace(v, reg))
		}
		return norm
	}
	return data
}
