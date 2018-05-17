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

	"github.com/oliveagle/jsonpath"
)

// Controller manages login and logout of an MD
type Controller struct {
	client http.Client
	url    string
	token  string
	reg    *regexp.Regexp
}

type loginResponse struct {
	GlobalResult struct {
		Status    string `json:"status"`
		StatusStr string `json:"status_str"`
		UIDARUBA  string `json:"UIDARUBA"`
	} `json:"_global_result"`
}

// NewController opens a session to a controller
func NewController(md, username, pass string, timeout time.Duration, skipVerify bool) (*Controller, error) {
	// Non-alphanumeric characters will get replaced by "_" in names
	reg, _ := regexp.Compile("[^a-zA-Z0-9]+")
	result := &Controller{
		client: http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: skipVerify},
			},
		},
		url: fmt.Sprintf("https://%s:4343/v1", md),
		reg: reg,
	}
	apiURL, data := result.url+"/api/login", url.Values{}
	data.Set("username", username)
	data.Set("password", pass)
	req, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Accept", "application/json")
	resp, err := result.client.Do(req)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("MD %s: Login incorrect (username %s)", md, username)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("MD %s: Could not read login response", md)
	}
	lr := loginResponse{}
	if err := json.Unmarshal(body, &lr); err != nil {
		return nil, fmt.Errorf("MD %s: Expected login response, got %s", md, string(body))
	}
	result.token = lr.GlobalResult.UIDARUBA
	return result, nil
}

// Logout closes the session.
func (c *Controller) Logout() error {
	apiURL := fmt.Sprintf("%s/api/logout?UIDARUBA=%s", c.url, c.token)
	resp, err := c.client.Get(apiURL)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("Logout returned error code %d (%s)", resp.StatusCode, resp.Status)
	}
	return nil
}

// Run runs a command on the controller, filters the output through the jsonpath expression, and gets the requested attribs
func (c *Controller) Run(cmd string, paths []*jsonpath.Compiled, attribs []string) ([]string, error) {
	apiURL := fmt.Sprintf("%s/configuration/showcommand?command=%s&json=1&UIDARUBA=%s",
		c.url, url.QueryEscape(cmd), c.token)
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Cookie", fmt.Sprintf("SESSION=%s", c.token))
	resp, err := c.client.Do(req)
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
	data = noWhitespace(data, c.reg)
	if paths != nil && len(paths) > 0 {
		for _, path := range paths {
			// Workaround for the jsonpath library to filter top-level arrays...
			// if the data is a top-level array, replace with an object with a single property, "_".
			// So your filters, instead of "$[...]" must be written "$._[...]"
			switch test := data.(type) {
			case []string:
				data = map[string]interface{}{"_": test}
			case []interface{}:
				data = map[string]interface{}{"_": test}
			}
			lookup, err := path.Lookup(data)
			if err != nil {
				return nil, err
			}
			data = lookup
		}
	}
	return toString(data, attribs)
}

// Switches lists the IP addresses of the switches that comply with the given jsonpath filters
// e.g. Switches("?(@.State=='up')") return switches up
func (c *Controller) Switches(filters []*jsonpath.Compiled) ([]string, error) {
	paths := make([]*jsonpath.Compiled, 0, len(filters)+1)
	// Prefilter, always on:
	first, err := jsonpath.Compile("$.All_Switches[?(@.Status == 'up')]")
	if err != nil {
		return nil, err
	}
	paths = append(paths, first)
	if filters != nil {
		paths = append(paths, filters...)
	}
	return c.Run("show switches", paths, []string{"IP_Address"})
}

// Switches asks the MM for its MDs
func Switches(md, username, pass string, filters []*jsonpath.Compiled, timeout time.Duration, skipVerify bool) ([]string, error) {
	controller, err := NewController(md, username, pass, timeout, skipVerify)
	if err != nil {
		return nil, err
	}
	defer controller.Logout()
	return controller.Switches(filters)
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

// toString turns the response into an array of lines
func toString(data interface{}, attribs []string) ([]string, error) {
	// Test if it is actually an array of strings
	var result []string
	switch data := data.(type) {
	case []string:
		result = data
	case string:
		result = []string{data}
	case []interface{}:
		result = make([]string, 0, len(data))
		for _, curr := range data {
			tmp, err := toString(curr, attribs)
			if err != nil {
				return nil, err
			}
			result = append(result, tmp...)
		}
	case map[string]interface{}:
		if attribs != nil && len(attribs) >= 0 {
			csv := make([]string, 0, len(attribs))
			for _, attr := range attribs {
				var val string
				if curr, ok := data[attr]; ok {
					switch curr := curr.(type) {
					case []byte:
						val = string(curr)
					case string:
						val = curr
					case int:
						val = string(val)
					case float32:
						val = string(val)
					case float64:
						val = string(val)
					default:
						val = "{Object}"
					}
				}
				csv = append(csv, val)
			}
			result = []string{strings.Join(csv, ";")}
		} else {
			out, err := json.MarshalIndent(data, "", "  ")
			if err != nil {
				return nil, err
			}
			result = []string{string(out)}
		}
	default:
		out, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return nil, err
		}
		result = []string{string(out)}
	}
	return result, nil
}
