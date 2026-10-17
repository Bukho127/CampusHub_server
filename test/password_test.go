package test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Bukho127/CampusHub_server/internal/password"
	"github.com/Bukho127/CampusHub_server/internal/users"
)

func TestPasswordHash(t *testing.T) {
	for _, plaintext := range []string{
		testPassword,
		"  " + testPassword + "  ",
		strings.Repeat("a", password.MinLength),
		strings.Repeat("a", password.MaxLength),
		strings.Repeat("界", 30), // More than 72 bytes: the full password must be hashed.
		strings.Repeat("界", password.MaxLength),
	} {
		hash, err := password.Hash(plaintext)
		if err != nil {
			t.Fatal(err)
		}
		other, err := password.Hash(plaintext)
		if err != nil {
			t.Fatal(err)
		}
		if hash == plaintext || hash == other || !strings.HasPrefix(hash, "$argon2id$") {
			t.Fatal("password hashes must be salted Argon2id values")
		}
		if matched, err := password.Verify(hash, plaintext); err != nil || !matched {
			t.Fatal("correct password did not verify")
		}
		if matched, err := password.Verify(hash, "wrong password"); err != nil || matched {
			t.Fatal("incorrect password verified")
		}
		if strings.HasPrefix(plaintext, " ") {
			if matched, err := password.Verify(hash, strings.TrimSpace(plaintext)); err != nil || matched {
				t.Fatal("password whitespace was not preserved")
			}
		}
	}
}

func TestPasswordRejectsInvalidInput(t *testing.T) {
	for _, plaintext := range []string{"", "short", strings.Repeat("a", 14), strings.Repeat("a", 129), "\xff" + testPassword} {
		hash, err := password.Hash(plaintext)
		if !errors.Is(err, password.ErrInvalidPassword) || hash != "" {
			t.Fatal("invalid password should not produce a hash")
		}
	}
}

func TestPasswordRejectsMalformedHashes(t *testing.T) {
	valid, err := password.Hash(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	for _, encoded := range []string{
		"", "plaintext", valid + "$extra",
		strings.Replace(valid, "argon2id", "argon2i", 1),
		strings.Replace(valid, "v=19", "v=16", 1),
		strings.Replace(valid, "m=19456", "m=4294967295", 1),
		strings.Replace(valid, "t=2", "t=99", 1),
		strings.Replace(valid, "p=1", "p=0", 1),
		valid[:len(valid)-1], valid[:len(valid)-1] + "!",
	} {
		if matched, err := password.Verify(encoded, testPassword); !errors.Is(err, password.ErrInvalidHash) || matched {
			t.Fatal("malformed or unsupported hash was accepted")
		}
	}
	if matched, err := password.Verify(valid, strings.Repeat("a", 1024)); err != nil || matched {
		t.Fatal("oversized verification input should not match")
	}
}

func TestUserPasswordIsPrivate(t *testing.T) {
	profile := users.User{ID: 1, Name: "Bukho", Email: "bukho@example.com", PasswordHash: "$argon2id$private"}
	repository := &userRepositoryStub{
		t:      t,
		getFn:  func(context.Context, int64) (users.User, error) { return profile, nil },
		listFn: func(context.Context, int, int) ([]users.User, error) { return []users.User{profile}, nil },
	}
	for _, path := range []string{"/api/v1/users/1", "/api/v1/users"} {
		response := userRequest(userRouter(repository), "GET", path, "", "")
		if response.Code != 200 {
			t.Fatal("profile request failed")
		}
		assertNoCredentials(t, response.Body.Bytes())
	}
	body, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	assertNoCredentials(t, body)
}

func TestUserPasswordWhitespace(t *testing.T) {
	plaintext := "  " + testPassword + "  "
	called := false
	repository := &userRepositoryStub{
		t: t,
		createFn: func(ctx context.Context, input users.CreateParams) (users.User, error) {
			called = true
			if matched, err := password.Verify(input.PasswordHash, plaintext); err != nil || !matched {
				t.Fatal("service modified the password before hashing")
			}
			return users.User{ID: 1, Name: input.Name, Email: input.Email}, nil
		},
	}
	response := userRequest(userRouter(repository), "POST", "/api/v1/users", userCreationBody("Bukho", "bukho@example.com", plaintext), "application/json")
	if response.Code != 201 || !called {
		t.Fatalf("create failed: %d %s", response.Code, response.Body)
	}
	assertNoCredentials(t, response.Body.Bytes())
}

func TestUserCreationStopsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := users.NewService(&userRepositoryStub{t: t})
	_, err := service.Create(ctx, users.Input{Name: "Bukho", Email: "bukho@example.com", Password: testPassword})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want canceled context", err)
	}
}
