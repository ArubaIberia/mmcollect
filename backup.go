package main

import (
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
	"github.com/pkg/errors"
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
		return errors.Wrapf(err, "Failed to lookup host '%s'", to.Host)
	}
	if len(ips) <= 0 {
		return errors.Errorf("Failed to resolve hostname '%s' to IP address", to.Host)
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
			return errors.Errorf("Dir path '%s' cannot contain double forward slashes", dir)
		}
		if !isValid(elem) {
			return errors.Errorf("Each component in dir path '%s' must match '%s'", dir, validRegexp)
		}
	}
	if !isValid(file) {
		return errors.Errorf("File name '%s' must match '%s'", file, validRegexp)
	}
	if err := c.Dial(); err != nil {
		return err
	}
	log.Print("Building flash backup...")
	flashFile, err := doBackup(c, file)
	if err != nil {
		return err
	}
	log.Print("Copying flash backup to remote server...")
	if err := doCopy(c, to.Scheme, host, to.User.Username(), pass, flashFile, dir, file); err != nil {
		return err
	}
	log.Print("Downloading flash backup...")
	if err := doRetrieve(to.Scheme, host, to.User.Username(), pass, dir, file); err != nil {
		return err
	}
	return nil
}

// doBackup performs the backup_flash api call and checks for errors
func doBackup(c *Controller, fileName string) (string, error) {
	baseFile, suffixes := "", []string{".tar.gz", ".tgz"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(fileName, suffix) {
			baseFile = strings.TrimSuffix(fileName, suffix)
			break
		}
	}
	if baseFile == "" {
		return "", errors.Errorf("Backup file name wrong suffix, must be one of '%s'", strings.Join(suffixes, "', '"))
	}
	result, err := c.Post("/md", "object/flash_backup", map[string]string{
		"backup_flash": "flash",
		// Not suported in AOS 8.2.2.2
		// "filename":     baseFile,
	})
	if err != nil {
		return "", err
	}
	lookup, err := NewLookup("$._global_result.status")
	if err != nil {
		return "", err
	}
	status, err := lookup.Lookup(result)
	if err != nil {
		return "", err
	}
	if status, ok := status.(float64); !ok || status != 0 {
		return "", errors.Errorf("Unexpected (float64) 0 value for backup _global_result.status: '%+v'", result)
	}
	// Not supported in AOS 8.2.2.2
	// return fmt.Sprintf("%s.tar.gz", baseFile), nil
	return "flashbackup.tar.gz", nil
}

// doCopy copies the flash backup to the external server
func doCopy(c *Controller, scheme, host, user, pass, flashFile, dir, file string) error {
	// This doesn't work through the API. The REST API always yields a 'wrong syntax' error
	cmd := fmt.Sprintf("copy flash: %s %s: %s %s %s %s", flashFile, scheme, host, user, dir, file)
	out, err := sshInteract(fmt.Sprintf("%s:22", c.IP()), c.username, c.password, cmd, pass)
	if err != nil {
		return err
	}
	if !strings.Contains(out, "File uploaded successfully") {
		return errors.Errorf("Failed to backup file, '%s'", strings.TrimSpace(out))
	}
	return nil
}

// doRetrieve retrieves the file from the external server
func doRetrieve(scheme, host, user, pass, dir, file string) error {
	// Only ftp currently supported
	if scheme != "ftp" {
		return errors.Errorf("Scheme '%s' is not supported for local retrieval", scheme)
	}
	conn, err := ftp.DialTimeout(fmt.Sprintf("%s:21", host), 5*time.Second)
	if err != nil {
		return errors.Wrapf(err, "Failed to connect to ftp server '%s'", host)
	}
	defer conn.Quit()
	if err := conn.Login(user, pass); err != nil {
		return errors.Wrapf(err, "Failed to login as user '%s'", user)
	}
	defer conn.Logout()
	if err := conn.ChangeDir(dir); err != nil {
		return errors.Wrapf(err, "Failed to change to folder '%s'", dir)
	}
	output, err := os.Create(file)
	if err != nil {
		return errors.Wrapf(err, "Failed to create output file '%s'", file)
	}
	defer output.Close()
	stream, err := conn.Retr(file)
	if err != nil {
		return errors.Wrapf(err, "Failed to retrieve file '%s'", file)
	}
	if err := func() error {
		defer stream.Close()
		if _, err := io.Copy(output, stream); err != nil {
			return errors.Wrapf(err, "Failed to save downloaded file '%s'", file)
		}
		return nil
	}(); err != nil {
		return err
	}
	if err := conn.Delete(file); err != nil {
		return errors.Wrapf(err, "Failed to remove remote file '%s'", file)
	}
	return nil
}
