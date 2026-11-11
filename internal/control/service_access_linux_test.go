//go:build linux

package control

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestServiceAccessUsesServiceIdentityAndPinnedInode(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to probe another identity")
	}
	for _, tc := range []struct {
		name         string
		owner, group int
		mode         os.FileMode
		groups       []uint32
		allowed      bool
	}{
		{"root private", 0, 0, 0700, nil, false},
		{"service private", 12345, 12345, 0700, nil, true},
		{"primary group", 0, 12345, 0750, nil, true},
		{"supplementary group", 0, 12346, 0750, []uint32{12346}, true},
		{"unrelated group", 0, 12346, 0750, nil, false},
		{"public", 0, 0, 0755, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir()
			if err := os.Chown(path, tc.owner, tc.group); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			access := serviceAccess{uid: 12345, gid: 12345, groups: tc.groups}
			if err := access.check(file, path, "-r", "-x"); (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v: %v", tc.allowed, err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "directory")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := os.Rename(path, path+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := (serviceAccess{uid: 12345, gid: 12345}).check(file, path, "-x"); err == nil {
		t.Fatal("checked replacement pathname instead of pinned inode")
	}
}

func TestServiceAccessHonorsACL(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root")
	}
	for _, allow := range []bool{false, true} {
		path := t.TempDir()
		// Linux POSIX ACL: owner, named user, group, mask, other. The named
		// entry must take precedence over otherwise permissive 'other' bits.
		acl := make([]byte, 4+5*8)
		binary.LittleEndian.PutUint32(acl, 2)
		userPerm, otherPerm := uint16(0), uint16(5)
		if allow {
			userPerm, otherPerm = 5, 0
		}
		entries := []struct {
			tag, perm uint16
			id        uint32
		}{
			{1, 7, ^uint32(0)}, {2, userPerm, 12345}, {4, 0, ^uint32(0)}, {16, 5, ^uint32(0)}, {32, otherPerm, ^uint32(0)},
		}
		for i, e := range entries {
			b := acl[4+i*8:]
			binary.LittleEndian.PutUint16(b, e.tag)
			binary.LittleEndian.PutUint16(b[2:], e.perm)
			binary.LittleEndian.PutUint32(b[4:], e.id)
		}
		if err := unix.Setxattr(path, "system.posix_acl_access", acl, 0); err != nil {
			if err == unix.EOPNOTSUPP {
				t.Skip("filesystem does not support POSIX ACLs")
			}
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		err = (serviceAccess{uid: 12345, gid: 12345}).check(file, path, "-r", "-x")
		file.Close()
		if (err == nil) != allow {
			t.Fatalf("ACL allow=%v: %v", allow, err)
		}
	}
}
