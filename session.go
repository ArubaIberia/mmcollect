package main

import (
	"bytes"
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

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
)

// Controller spawns sessions to a Managed Device
type Controller struct {
	client   *http.Client
	url      string
	username string
	password string
	md       string
	reg      *regexp.Regexp
	useSSH   bool
	// Token for API access, and when was it last used
	lastToken string
	lastUsed  time.Time
	expires   time.Time
	// Client for SSH access, and when was it last used
	sshClient *ssh.Client
	lastSSH   time.Time
}

type loginResponse struct {
	GlobalResult struct {
		Status    string `json:"status"`
		StatusStr string `json:"status_str"`
		UIDARUBA  string `json:"UIDARUBA"`
	} `json:"_global_result"`
}

// NewController opens a session to a controller
func NewController(md, username, pass string, client *http.Client, useSSH bool) *Controller {
	// Non-alphanumeric characters will get replaced by "_" in names
	reg, _ := regexp.Compile("[^a-zA-Z0-9]+")
	return &Controller{
		client:   client,
		url:      fmt.Sprintf("https://%s:4343/v1", md),
		reg:      reg,
		username: username,
		password: pass,
		md:       md,
		useSSH:   useSSH,
	}
}

// IP returns the address of the controller
func (c *Controller) IP() string {
	return c.md
}

// Login opens a new session to the controller
func (c *Controller) login() (token string, expires time.Time, err error) {
	apiURL, data := fmt.Sprintf("%s/api/login", c.url), url.Values{}
	parsedURL, err := url.Parse(apiURL)
	if err != nil {
		return token, expires, errors.Wrapf(err, "Parsing login URL '%s' failed", apiURL)
	}
	data.Set("username", c.username)
	data.Set("password", c.password)
	req, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return token, expires, errors.Wrapf(err, "Building request for '%s' failed", apiURL)
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Accept", "application/json")
	resp, err := c.client.Do(req)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return token, expires, errors.Wrapf(err, "Login request to MD '%s' failed", c.md)
	}
	if resp.StatusCode != 200 {
		return token, expires, errors.Errorf("MD '%s': Login incorrect (username '%s')", c.md, c.username)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return token, expires, errors.Errorf("MD '%s': Could not read login response", c.md)
	}
	lr := loginResponse{}
	if err := json.Unmarshal(body, &lr); err != nil {
		return token, expires, errors.Errorf("MD '%s': Expected login response, got '%s'", c.md, string(body))
	}
	token = lr.GlobalResult.UIDARUBA
	for _, cookie := range c.client.Jar.Cookies(parsedURL) {
		if cookie.Name == "SESSION" {
			expires = cookie.Expires
			return token, expires, nil
		}
	}
	return token, expires, errors.Errorf("MD '%s': No SESSION cookie received", c.md)
}

// sshLogin opens a new session to the controller
func (c *Controller) sshLogin() (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User: c.username,
		Auth: []ssh.AuthMethod{
			ssh.Password(c.password),
		},
		// I'm not managing ssh keys as of now
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:22", c.md), config)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to dial SSH to '%s'", c.md)
	}
	return client, nil
}

func (c *Controller) logout() error {
	defer func() {
		// Make sure we clean the struct no matter what
		c.lastUsed = time.Time{}
		c.lastToken = ""
		c.expires = time.Time{}
	}()
	apiURL := fmt.Sprintf("%s/api/logout?UIDARUBA=%s", c.url, c.lastToken)
	resp, err := c.client.Get(apiURL)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return errors.Wrapf(err, "Failed to perform logout request to '%s'", c.md)
	}
	if resp.StatusCode != 200 {
		return errors.Errorf("Logout returned error code %d (%s)", resp.StatusCode, resp.Status)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrap(err, "Could not read body of logout response")
	}
	if !strings.Contains(string(body), "You've been logged out successfully.") {
		return errors.New(string(body))
	}
	return nil
}

func (c *Controller) sshLogout() error {
	defer func() {
		c.sshClient = nil
		c.lastSSH = time.Time{}
	}()
	if c.sshClient != nil {
		return errors.WithStack(c.sshClient.Close())
	}
	return nil
}

// Dial an SSH API session, before running
func (c *Controller) Dial() error {
	// ASlways dial the API
	now := time.Now()
	if c.lastUsed.IsZero() || (!c.expires.IsZero() && c.expires.Before(now)) || (now.Sub(c.lastUsed).Minutes() > 5) {
		if c.lastToken != "" {
			c.logout()
		}
		token, expires, err := c.login()
		if err != nil {
			return err
		}
		c.lastToken = token
		c.expires = expires
	}
	c.lastUsed = now
	// SSH is only dialed on demand
	if c.useSSH {
		return c.sshDial(now)
	}
	return nil
}

func (c *Controller) sshDial(now time.Time) error {
	if (c.sshClient == nil) || c.lastSSH.IsZero() || (now.Sub(c.lastSSH).Minutes() > 5) {
		if c.sshClient != nil {
			c.sshLogout()
		}
		client, err := c.sshLogin()
		if err != nil {
			return err
		}
		c.sshClient = client
	}
	c.lastSSH = now
	return nil
}

// Close the controller
func (c *Controller) Close() error {
	var err error
	if c.lastToken != "" {
		if err1 := c.logout(); err1 != nil {
			err = err1
		}
	}
	if c.sshClient != nil {
		if err2 := c.sshLogout(); err2 != nil && err == nil {
			err = err2
		}
	}
	if err != nil {
		// A common pattern will be just defer session.Close()
		// I don't want the error message to go unnoticed
		log.Println("Error closing session to ", c.md, ": ", err)
	}
	return err
}

// Get request
func (c *Controller) Get(cfgPath, endpoint string, data interface{}) (interface{}, error) {
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
		return nil, errors.Errorf("Invalid params type: %T", data)
	}
	return c.apiRequest(http.MethodGet, cfgPath, endpoint, params, nil)
}

// Post request
func (c *Controller) Post(cfgPath, endpoint string, data interface{}) (interface{}, error) {
	var body io.Reader
	if data != nil {
		marshaled, err := json.Marshal(data)
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to marshal data to json: '%+v'", data)
		}
		body = bytes.NewReader(marshaled)
	}
	return c.apiRequest(http.MethodPost, cfgPath, endpoint, nil, body)
}

// Show runs a show command on the controller
func (c *Controller) Show(cmd string, path Lookup) (interface{}, error) {
	var result interface{}
	var err error
	if !c.useSSH {
		// Run the command via API
		result, err = c.Get("/mm", "showcommand", map[string]string{"command": cmd})
		if err != nil {
			return nil, err
		}
		if path != nil {
			lookup, err := path.Lookup(result)
			if err != nil {
				return nil, err
			}
			return lookup, err
		}
		return result, err
	}
	// Run the command via SSH
	if path != nil {
		filter, err := path.ForSSH()
		if err != nil {
			return nil, err
		}
		cmd = strings.Join([]string{cmd, filter}, " | ")
	}
	sshSession, err := c.sshClient.NewSession()
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to create SSH session to '%s'", c.md)
	}
	defer sshSession.Close()
	// Once a Session is created, you can execute a single command on
	// the remote side using the Run method.
	var b, e bytes.Buffer
	sshSession.Stdout = &b
	sshSession.Stderr = &e
	if err := sshSession.Run(cmd); err != nil {
		return nil, errors.Wrapf(err, "Failed to Run SSH command on '%s'", c.md)
	}
	data := strings.Split(b.String(), "\n")
	return append(data, strings.Split(e.String(), "\n")...), nil
}

func (c *Controller) apiRequest(method, cfgPath, endpoint string, params map[string]string, body io.Reader) (interface{}, error) {
	if strings.HasPrefix(endpoint, "/") {
		endpoint = endpoint[1:]
	}
	textURL := fmt.Sprintf("%s/configuration/%s", c.url, endpoint)
	apiURL, err := url.Parse(textURL)
	if err != nil {
		return "", errors.Wrapf(err, "Failed to parse url '%s'", textURL)
	}
	query := apiURL.Query()
	query.Set("config_path", cfgPath)
	query.Set("json", "1")
	query.Set("UIDARUBA", c.lastToken)
	if params != nil {
		for k, v := range params {
			query.Set(k, v)
		}
	}
	apiURL.RawQuery = query.Encode()
	req, err := http.NewRequest(method, apiURL.String(), body)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to build request for md '%s'", c.md)
	}
	if body != nil {
		req.Header.Add("Content-Type", "application/json")
	}
	req.Header.Add("Cookie", fmt.Sprintf("SESSION=%s", c.lastToken))
	req.Header.Add("Accept", "application/json")
	resp, err := c.client.Do(req)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to run request from md '%s'", c.md)
	}
	if resp.StatusCode != 200 {
		return nil, errors.Errorf("%s '%s' returned error code %d", method, apiURL.String(), resp.StatusCode)
	}
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to read response body from md '%s'", c.md)
	}
	var data interface{}
	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		return nil, errors.Wrapf(err, "Failed to decode data '%s'", string(bodyBytes))
	}
	result := noWhitespace(data, c.reg)
	return result, nil
}

// Switches lists the IP addresses of the switches that comply with the given filter
// e.g. Switches("?(@.State=='up')") return switches up
func (c *Controller) Switches(filter Lookup) ([]string, error) {
	if c.useSSH {
		return nil, errors.New("Switches can only be listed via API, not SSH")
	}
	// Prefilter, always on:
	path, err := NewLookup("$.All_Switches[?(@.Status == 'up')]")
	if err != nil {
		return nil, err
	}
	if filter != nil {
		path = append(path, filter)
	}
	if err := c.Dial(); err != nil {
		return nil, err
	}
	data, err := c.Show("show switches", path)
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
