// Package envfile loads KEY=VALUE lines from a .env file into the process
// environment. Variables already set in the environment are kept, so an
// explicit export always wins over the file.
package envfile

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// Load reads path if it exists. A missing file is not an error. It returns
// the names of the variables it set. Values are never logged or returned.
func Load(path string) ([]string, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var set []string
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		s := strings.TrimSpace(sc.Text())
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		s = strings.TrimPrefix(s, "export ")
		key, val, ok := strings.Cut(s, "=")
		if !ok {
			return set, fmt.Errorf("%s line %d: expected KEY=VALUE", path, line)
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if key == "" {
			return set, fmt.Errorf("%s line %d: empty variable name", path, line)
		}
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return set, err
		}
		set = append(set, key)
	}
	return set, sc.Err()
}
