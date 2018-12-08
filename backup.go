package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
	"golang.org/x/crypto/ssh/terminal"
)

// Backup the flash of the MM
// URL is an FTP URL to store the backup there, e.g.
// ftp://user:pass@host/path/filename.tar.gz
func (c *Controller) Backup(to *url.URL) error {
	if to.Scheme == "" {
		return errors.New("Missing scheme for backup")
	}
	if to.Host == "" {
		return errors.New("Missing host for backup")
	}
	ips, err := net.LookupIP(to.Host)
	if err != nil {
		return decorate(err, "Failed to lookup host", to.Host)
	}
	if len(ips) <= 0 {
		return fmt.Errorf("Failed to resolve hostname %s to IP address", to.Host)
	}
	host := ips[0].String()
	log.Printf("Backup server host resolved to %s", host)
	if to.User == nil || to.User.Username() == "" {
		return errors.New("Missing user for backup")
	}
	pass, ok := to.User.Password()
	if !ok {
		fmt.Fprint(os.Stderr, "Password for backup: ")
		passBytes, err := terminal.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			return err
		}
		pass = string(passBytes)
		fmt.Fprintln(os.Stderr, "")
	}
	if to.Path == "" {
		return errors.New("Missing path for backup")
	}
	dir, file := path.Split(path.Clean(to.Path))
	if dir == "" {
		return errors.New("Missing dir name for backup")
	}
	if file == "" {
		return errors.New("Missing file name for backup")
	}
	const validRegexp = `^[a-zA-Z0-9\_][a-zA-Z0-9\.\_-]*$`
	isValid := regexp.MustCompile(validRegexp).MatchString
	// Check each component of the path
	dir = strings.Trim(dir, "/")
	for _, elem := range strings.Split(dir, "/") {
		if elem == "" {
			return fmt.Errorf("Dir path '%s' cannot contain double forward slashes", dir)
		}
		if !isValid(elem) {
			return fmt.Errorf("Each componen in dir path '%s' must match '%s'", dir, validRegexp)
		}
	}
	if !isValid(file) {
		return fmt.Errorf("File name '%s' must match '%s'", file, validRegexp)
	}
	s, err := c.Session()
	if err != nil {
		return err
	}
	defer s.Close()
	log.Print("Building flash backup...")
	flashFile, err := doBackup(s, file)
	if err != nil {
		return err
	}
	log.Print("Copying flash backup to remote server...")
	if err := doCopy(s, to.Scheme, host, to.User.Username(), pass, flashFile, dir, file); err != nil {
		return err
	}
	log.Print("Downloading flash backup...")
	if err := doRetrieve(to.Scheme, host, to.User.Username(), pass, dir, file); err != nil {
		return err
	}
	return nil
}

// doBackup performs the backup_flash api call and checks for errors
func doBackup(s *Session, fileName string) (string, error) {
	baseFile, suffixes := "", []string{".tar.gz", ".tgz"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(fileName, suffix) {
			baseFile = strings.TrimSuffix(fileName, suffix)
		}
	}
	if baseFile == "" {
		return "", fmt.Errorf("Backup file name wrong suffix, must be one of '%s'", strings.Join(suffixes, "', '"))
	}
	result, err := s.Post("/md", "object/flash_backup", map[string]string{
		"backup_flash": "flash",
		"filename":     baseFile,
	})
	if err != nil {
		return "", decorate(err, "Failed to post backup request")
	}
	lookup, err := NewLookup("$._global_result.status")
	if err != nil {
		return "", decorate(err, "Failed to parse lookup expression")
	}
	status, err := lookup.Lookup(result)
	if err != nil {
		return "", decorate(err, "Failed to retrieve status", result)
	}
	if status, ok := status.(float64); !ok || status != 0 {
		return "", fmt.Errorf("Unexpected backup _global_result.status: %+v", result)
	}
	return fmt.Sprintf("%s.tar.gz", baseFile), nil
}

// doCopy copies the flash backup to the external server
func doCopy(s *Session, scheme, host, user, pass, flashFile, dir, file string) error {
	// This doesn't work through the API. The REST API always yields a 'wrong syntax' error
	cmd := fmt.Sprintf("copy flash: %s %s: %s %s %s %s", flashFile, scheme, host, user, dir, file)
	mm := s.Controller()
	out, err := sshInteract(fmt.Sprintf("%s:22", mm.IP()), mm.username, mm.password, cmd, pass)
	if err != nil {
		return err
	}
	if !strings.Contains(out, "File uploaded successfully") {
		return fmt.Errorf("Failed to backup file, %s", strings.TrimSpace(out))
	}
	return nil
}

// doRetrieve retrieves the file from the external server
func doRetrieve(scheme, host, user, pass, dir, file string) error {
	// Only ftp currently supported
	if scheme != "ftp" {
		return fmt.Errorf("Scheme %s is not supported for local retrieval", scheme)
	}
	conn, err := ftp.DialTimeout(fmt.Sprintf("%s:21", host), 5*time.Second)
	if err != nil {
		return decorate(err, "Failed to connect to ftp server", host)
	}
	defer conn.Quit()
	if err := conn.Login(user, pass); err != nil {
		return decorate(err, "Failed to login to ftp server", user)
	}
	defer conn.Logout()
	if err := conn.ChangeDir(dir); err != nil {
		return decorate(err, "Failed to change to folder", dir)
	}
	output, err := os.Create(file)
	if err != nil {
		return decorate(err, "Failed to create output file", file)
	}
	defer output.Close()
	stream, err := conn.Retr(file)
	if err != nil {
		return decorate(err, "Failed to retrieve file", file)
	}
	if err := func() error {
		defer stream.Close()
		if _, err := io.Copy(output, stream); err != nil {
			return decorate(err, "Failed to save downloaded file", file)
		}
		return nil
	}(); err != nil {
		return err
	}
	if err := conn.Delete(file); err != nil {
		return decorate(err, "Failed to remove remote file", file)
	}
	return nil
}
