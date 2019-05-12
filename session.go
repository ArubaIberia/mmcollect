package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Controller spawns sessions to a Managed Device
type Controller struct {
	client    *http.Client
	url       string
	username  string
	password  string
	md        string
	reg       *regexp.Regexp
	lastToken string
	lastUsed  time.Time
	expires   time.Time
}

// Session encapsulates a session to the Managed Device
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
func NewController(md, username, pass string, client *http.Client) *Controller {
	// Non-alphanumeric characters will get replaced by "_" in names
	reg, _ := regexp.Compile("[^a-zA-Z0-9]+")
	return &Controller{
		client:   client,
		url:      fmt.Sprintf("https://%s:4343/v1", md),
		reg:      reg,
		username: username,
		password: pass,
		md:       md,
	}
}

// IP returns the address of the controller
func (c *Controller) IP() string {
	return c.md
}

// Session opens a new session to the controller
func (c *Controller) login() error {
	apiURL, data := fmt.Sprintf("%s/api/login", c.url), url.Values{}
	parsedURL, err := url.Parse(apiURL)
	if err != nil {
		return decorate(err, "Parsing login URL", apiURL, "failed")
	}
	data.Set("username", c.username)
	data.Set("password", c.password)
	req, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return decorate(err, "Building request for", apiURL, "failed")
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Accept", "application/json")
	resp, err := c.client.Do(req)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return decorate(err, "Login request to MD", c.md, "failed")
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("MD %s: Login incorrect (username %s)", c.md, c.username)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("MD %s: Could not read login response", c.md)
	}
	lr := loginResponse{}
	if err := json.Unmarshal(body, &lr); err != nil {
		return fmt.Errorf("MD %s: Expected login response, got %s", c.md, string(body))
	}
	c.lastToken = lr.GlobalResult.UIDARUBA
	for _, cookie := range c.client.Jar.Cookies(parsedURL) {
		if cookie.Name == "SESSION" {
			c.expires = cookie.Expires
			return nil
		}
	}
	return fmt.Errorf("MD %s: No SESSION cookie received", c.md)
}

func (c *Controller) logout() error {
	apiURL := fmt.Sprintf("%s/api/logout?UIDARUBA=%s", c.url, c.lastToken)
	resp, err := c.client.Get(apiURL)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return decorate(err, "Failed to perform logout request to", c.md)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("Logout returned error code %d (%s)", resp.StatusCode, resp.Status)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return decorate(err, "Could not read body of logout response")
	}
	if !strings.Contains(string(body), "You've been logged out successfully.") {
		return errors.New(string(body))
	}
	return nil
}

// Session opens a new session to the controller
func (c *Controller) Session() (*Session, error) {
	now := time.Now()
	if c.lastUsed.IsZero() || (!c.expires.IsZero() && c.expires.Before(now)) || (now.Sub(c.lastUsed).Minutes() > 5) {
		if c.lastToken != "" {
			c.logout()
		}
		if err := c.login(); err != nil {
			return nil, err
		}
	}
	c.lastUsed = now
	return &Session{controller: c, token: c.lastToken}, nil
}

// Close the controller
func (c *Controller) Close() error {
	if c.lastToken == "" {
		return nil
	}
	err := c.logout()
	if err != nil {
		// A common pattern will be just defer session.Close()
		// I don't want the error message to go unnoticed
		log.Println("Error closing session to ", c.md, ": ", err)
	}
	return err
}

// Close the session
func (s *Session) Close() error {
	// Does nothing. Sessions are cached in the Controller object.
	return nil
}

// Get request
func (s *Session) Get(cfgPath, endpoint string, data interface{}) (interface{}, error) {
	var params map[string]string
	switch data := data.(type) {
	case map[string]string:
		params = data
	case map[string]interface{}:
		params := make(map[string]string)
		for k, v := range data {
			switch v := v.(type) {
			case string:
				params[k] = v
			default:
				params[k] = fmt.Sprintf("%s", v)
			}
		}
	default:
		return nil, fmt.Errorf("Invalid params type: %T", data)
	}
	return s.apiRequest(http.MethodGet, cfgPath, endpoint, params, nil)
}

// Post request
func (s *Session) Post(cfgPath, endpoint string, data interface{}) (interface{}, error) {
	var body io.Reader
	if data != nil {
		marshaled, err := json.Marshal(data)
		if err != nil {
			return nil, decorate(err, "Failed to marshal data to json,", data)
		}
		body = bytes.NewReader(marshaled)
	}
	return s.apiRequest(http.MethodPost, cfgPath, endpoint, nil, body)
}

// Show runs a show command on the controller
func (s *Session) Show(cmd string, path Lookup) (interface{}, error) {
	result, err := s.Get("/mm", "showcommand", map[string]string{"command": cmd})
	if err != nil {
		return nil, decorate(err, "Failed to GET show command from ", s.controller.md)
	}
	if path != nil {
		lookup, err := path.Lookup(result)
		if err != nil {
			return nil, err
		}
		result = lookup
	}
	return result, nil
}

func (s *Session) apiRequest(method, cfgPath, endpoint string, params map[string]string, body io.Reader) (interface{}, error) {
	if strings.HasPrefix(endpoint, "/") {
		endpoint = endpoint[1:]
	}
	textURL := fmt.Sprintf("%s/configuration/%s", s.controller.url, endpoint)
	apiURL, err := url.Parse(textURL)
	if err != nil {
		return "", decorate(err, "Failed to parse url", textURL)
	}
	query := apiURL.Query()
	query.Set("config_path", cfgPath)
	query.Set("json", "1")
	query.Set("UIDARUBA", s.token)
	if params != nil {
		for k, v := range params {
			query.Set(k, v)
		}
	}
	apiURL.RawQuery = query.Encode()
	req, err := http.NewRequest(method, apiURL.String(), body)
	if err != nil {
		return nil, decorate(err, "Failed to build request for md", s.controller.md)
	}
	if body != nil {
		req.Header.Add("Content-Type", "application/json")
	}
	req.Header.Add("Cookie", fmt.Sprintf("SESSION=%s", s.token))
	req.Header.Add("Accept", "application/json")
	resp, err := s.controller.client.Do(req)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, decorate(err, "Failed to run request from md", s.controller.md)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s '%s' returned error code %d", method, apiURL.String(), resp.StatusCode)
	}
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, decorate(err, "Failed to read response body from md", s.controller.md)
	}
	var data interface{}
	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		return nil, decorate(err, "Failed to decode data", string(bodyBytes))
	}
	result := noWhitespace(data, s.controller.reg)
	return result, nil
}

// Controller for this session
func (s *Session) Controller() *Controller {
	return s.controller
}

// Switches lists the IP addresses of the switches that comply with the given filter
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
			// Remove also trailing underscores
			for strings.HasSuffix(k, "_") {
				k = k[:len(k)-1]
			}
			norm[k] = noWhitespace(v, reg)
		}
		return norm
	case []interface{}:
		for i, v := range data {
			data[i] = noWhitespace(v, reg)
		}
		// This "data" here is the typed one! it is masked
		// because of the type switch.
		return data
	}
	return data
}
