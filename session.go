package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
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
	client   http.Client
	url      string
	username string
	password string
	md       string
	reg      *regexp.Regexp
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

// IP returns the address of the controller
func (c *Controller) IP() string {
	return c.md
}

// Session opens a new session to the controller
func (c *Controller) Session() (*Session, error) {
	apiURL, data := fmt.Sprintf("%s/api/login", c.url), url.Values{}
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
	err := s.close()
	if err != nil {
		// A common pattern will be just defer session.Close()
		// I don't want the error message to go unnoticed
		log.Println("Error clossing session to ", s.controller.md, ": ", err)
	}
	return err
}

func (s *Session) close() error {
	apiURL := fmt.Sprintf("%s/api/logout?UIDARUBA=%s", s.controller.url, s.token)
	resp, err := s.controller.client.Get(apiURL)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("Logout returned error code %d (%s)", resp.StatusCode, resp.Status)
	}
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
			return nil, err
		}
		body = bytes.NewReader(marshaled)
	}
	return s.apiRequest(http.MethodPost, cfgPath, endpoint, nil, body)
}

// Show runs a show command on the controller
func (s *Session) Show(cmd string, path Lookup) (interface{}, error) {
	result, err := s.Get("/mm", "showcommand", map[string]string{"command": cmd})
	if err != nil {
		return nil, err
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
	apiURL, err := url.Parse(fmt.Sprintf("%s/configuration/%s", s.controller.url, endpoint))
	if err != nil {
		return "", err
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
		return nil, err
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
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s '%s' returned error code %d", method, apiURL.String(), resp.StatusCode)
	}
	dec := json.NewDecoder(resp.Body)
	var data interface{}
	if err := dec.Decode(&data); err != nil {
		return nil, err
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
