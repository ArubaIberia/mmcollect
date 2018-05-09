package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/oliveagle/jsonpath"
)

// Controller manages login and logout of an MD
type Controller struct {
	client http.Client
	url    string
	token  string
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
	result := &Controller{
		client: http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: skipVerify},
			},
		},
		url: fmt.Sprintf("https://%s:4343/v1", md),
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

// Run runs a command on the controller, returns the output
func (c *Controller) Run(cmd string, path *jsonpath.Compiled) ([]string, error) {
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
	if path != nil {
		lookup, err := path.Lookup(data)
		if err != nil {
			return nil, err
		}
		data = lookup
	}
	return toString(data)
}

// Switches lists the IP addresses of the switches that comply with the given jsonpath filter
// e.g. Switches("?(@.State=='up')") return switches up
func (c *Controller) Switches(filter string) ([]string, error) {
	// Take the IP of those switches that match the filter
	path, err := jsonpath.Compile(fmt.Sprintf("$.All Switches[%s].IP Address", filter))
	if err != nil {
		return nil, err
	}
	addresses, err := c.Run("show switches", path)
	if err != nil {
		return nil, err
	}
	return toString(addresses)
}

// Switches asks the MM for its MDs
func Switches(md, username, pass, filter string, timeout time.Duration, skipVerify bool) ([]string, error) {
	controller, err := NewController(md, username, pass, timeout, skipVerify)
	if err != nil {
		return nil, err
	}
	defer controller.Logout()
	return controller.Switches(filter)
}

// toString turns the response into an array of lines
func toString(data interface{}) ([]string, error) {
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
			tmp, err := toString(curr)
			if err != nil {
				return nil, err
			}
			result = append(result, tmp...)
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
