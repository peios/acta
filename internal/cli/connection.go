package cli

import "acta/internal/client"

// Connection is a startup snapshot: changing the active profile cannot redirect
// a running native process. The credential stays in the shared client only.
type Connection struct {
	Profile   string
	ConfigDir string
	Client    *client.Client
}

func OpenConnection(profile string) (Connection, error) {
	dir, err := configDir()
	if err != nil {
		return Connection{}, err
	}
	a := &App{repo: Repository{Dir: dir}, vault: newVault(dir), profile: profile}
	name, p, err := a.selected()
	if err != nil {
		return Connection{}, err
	}
	c, err := a.clientForProfile(p)
	if err != nil {
		return Connection{}, err
	}
	c.URL, err = client.NormalizeURL(c.URL)
	if err != nil {
		return Connection{}, err
	}
	return Connection{Profile: name, ConfigDir: dir, Client: c}, nil
}
