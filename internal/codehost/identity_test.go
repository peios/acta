package codehost

import "testing"

func TestIdentityPersistenceIsolationAndLock(t *testing.T) {
	dir := t.TempDir()
	id, close, err := LockIdentity(dir, "https://acta.example", "one")
	if err != nil {
		t.Fatal(err)
	}
	if _, unlock, err := LockIdentity(dir, "https://acta.example", "one"); err == nil {
		unlock()
		t.Fatal("duplicate process allowed")
	}
	close()
	again, close, err := LockIdentity(dir, "https://acta.example", "one")
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	if again != id {
		t.Fatal("identity changed after restart")
	}
	for _, pair := range [][2]string{{"https://acta.example", "two"}, {"https://other.example", "one"}} {
		next, unlock, err := LockIdentity(dir, pair[0], pair[1])
		if err != nil {
			t.Fatal(err)
		}
		unlock()
		if next == id {
			t.Fatal("identity crossed account/server boundary")
		}
	}
}
