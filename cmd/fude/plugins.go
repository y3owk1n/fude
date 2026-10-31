package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/internal/install"
	"github.com/y3owk1n/fude/internal/keep"
	"github.com/y3owk1n/fude/plugin"
)

// repos are the repos of tables.
func repos(tables []editor.RepoPlugin) []string {
	out := make([]string, len(tables))
	for i, t := range tables {
		out[i] = t.Repo
	}
	return out
}

// pluginInstall installs each plugin of a repo config.toml lists that is
// not installed, and prints each it installs (B601).
func pluginInstall() error {
	tables, err := editor.RepoPlugins()
	if err != nil {
		return err
	}
	lock, err := install.Read()
	if err != nil {
		return err
	}
	var errs []error
	n := 0
	for _, t := range tables {
		locked, _ := lock.Find(t.Repo)
		if _, ok := install.Installed(locked, t.Version); ok {
			continue
		}
		fmt.Printf("installing %s\n", install.Name(t.Repo))
		e, m, err := install.Install(context.Background(), t.Repo, t.Version, locked, version())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		lock.Put(e, repos(tables))
		fmt.Printf("installed %s %s\n", m.Name, install.Version(e))
		n++
	}
	if n == 0 && len(errs) == 0 {
		fmt.Println("every plugin of a repo is installed")
		return nil
	}
	if n > 0 {
		errs = append(errs, lock.Write(), install.Prune(repos(tables)))
	}
	return errors.Join(errs...)
}

// pluginUpdate updates each plugin of a repo config.toml lists, or the
// one called name, as :plugin update does, and prints what it did (B601).
func pluginUpdate(name string) error {
	tables, err := editor.RepoPlugins()
	if err != nil {
		return err
	}
	lock, err := install.Read()
	if err != nil {
		return err
	}
	var errs []error
	found, n := false, 0
	for _, t := range tables {
		locked, _ := lock.Find(t.Repo)
		if name != "" && name != install.Name(t.Repo) && name != installedName(locked) {
			continue
		}
		found = true
		if install.Pinned(t.Version) {
			fmt.Printf("%s is pinned to %s\n", install.Name(t.Repo), t.Version)
			continue
		}
		e, m, err := install.Update(context.Background(), t.Repo, t.Version, t.Capabilities, locked, version())
		switch {
		case err != nil:
			errs = append(errs, err)
		case e.Commit != locked.Commit:
			lock.Put(e, repos(tables))
			fmt.Println(install.Updated(m.Name, locked, e))
			n++
		}
	}
	switch {
	case name != "" && !found:
		return errors.New("no plugin of a repo is called " + name)
	case n == 0 && len(errs) == 0:
		fmt.Println("every plugin is up to date")
	case n > 0:
		errs = append(errs, lock.Write(), install.Prune(repos(tables)))
	}
	return errors.Join(errs...)
}

// installedName is the name the installed plugin locked gives itself, or
// "" when none is installed.
func installedName(locked install.Entry) string {
	p, ok := install.Installed(locked, "")
	if !ok {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	m, err := plugin.ReadManifest(b)
	if err != nil {
		return ""
	}
	return m.Name
}

// pluginAdd installs the plugin of repo, as repo or repo@version, prints
// what its manifest asks for, and once the user answers y, appends its
// table to config.toml (B601).
func pluginAdd(arg string) error {
	repo, ver := arg, ""
	// A git URL such as git@host:owner/name has an @ that is no version's.
	if i := strings.LastIndex(arg, "@"); i >= 0 && !strings.ContainsAny(arg[i:], "/:") {
		repo, ver = arg[:i], arg[i+1:]
	}
	tables, err := editor.RepoPlugins()
	if err != nil {
		return err
	}
	if slices.Contains(repos(tables), repo) {
		return fmt.Errorf("config.toml already installs %s", repo)
	}
	fmt.Printf("getting %s\n", install.Name(repo))
	e, m, wasm, err := install.Fetch(context.Background(), repo, ver, install.Entry{}, version())
	if err != nil {
		return err
	}
	fmt.Printf("%s %s asks for\n", m.Name, install.Version(e))
	for _, l := range []struct {
		what string
		list []string
	}{{"capabilities", m.Capabilities}, {"programs", m.Programs}, {"hosts", m.Hosts}} {
		fmt.Printf("  %s: %s\n", l.what, listed(l.list))
	}
	fmt.Print("Add its table to config.toml? [y/N] ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.TrimSpace(strings.ToLower(answer)) != "y" {
		fmt.Println("added nothing")
		return nil
	}
	lock, err := install.Read()
	if err != nil {
		return err
	}
	if err := install.Store(e, wasm); err != nil {
		return err
	}
	lock.Put(e, append(repos(tables), repo))
	if err := lock.Write(); err != nil {
		return err
	}
	dir, err := keep.Dir("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return fmt.Errorf("add %s: %w", repo, err)
	}
	path := filepath.Join(dir, "fude", "config.toml")
	if err := appendTable(path, repo, ver, m); err != nil {
		return fmt.Errorf("add %s to %s: %w", repo, path, err)
	}
	fmt.Printf("added %s to %s, and :plugin reload loads it in a fude already running\n", m.Name, path)
	return nil
}

// listed is list as the prompt shows it, or none.
func listed(list []string) string {
	if len(list) == 0 {
		return "none"
	}
	return strings.Join(list, ", ")
}

// appendTable appends to the file at path a [[plugin]] table that
// installs repo at ver and grants what m asks for, and changes nothing
// else in it.
func appendTable(path, repo, ver string, m plugin.Manifest) error {
	var b strings.Builder
	fmt.Fprintf(&b, "\n[[plugin]]\nrepo = %q\n", repo)
	if ver != "" {
		fmt.Fprintf(&b, "version = %q\n", ver)
	}
	for _, l := range []struct {
		key  string
		list []string
	}{{"capabilities", m.Capabilities}, {"programs", m.Programs}, {"hosts", m.Hosts}} {
		if len(l.list) == 0 && l.key != "capabilities" {
			continue
		}
		quoted := make([]string, len(l.list))
		for i, s := range l.list {
			quoted[i] = fmt.Sprintf("%q", s)
		}
		fmt.Fprintf(&b, "%s = [%s]\n", l.key, strings.Join(quoted, ", "))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(b.String())
	return errors.Join(err, f.Close())
}
