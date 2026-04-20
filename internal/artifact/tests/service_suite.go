// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tests

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/genai"

	"google.golang.org/adk/artifact"
)

func TestArtifactService(t *testing.T, name string, factory func(t *testing.T) (artifact.Service, error)) {
	t.Run(fmt.Sprintf("Test%sArtifactService", name), func(t *testing.T) {
		ctx := t.Context()
		// Create the service using the factory for this sub-test
		srv, err := factory(t)
		if err != nil {
			t.Fatalf("Failed to set up service: %v", err)
		}
		testArtifactService(ctx, t, srv, name)
	})
	t.Run(fmt.Sprintf("Test%sArtifactService_Empty", name), func(t *testing.T) {
		ctx := t.Context()
		// Create the service using the factory for this sub-test
		srv, err := factory(t)
		if err != nil {
			t.Fatalf("Failed to set up service: %v", err)
		}
		testArtifactService_Empty(ctx, t, srv, name)
	})
	t.Run(fmt.Sprintf("Test%sArtifactService_UserScoped", name), func(t *testing.T) {
		ctx := t.Context()
		// Create the service using the factory for this sub-test
		srv, err := factory(t)
		if err != nil {
			t.Fatalf("Failed to set up service: %v", err)
		}
		testArtifactService_UserScoped(ctx, t, srv, name)
	})
	t.Run(fmt.Sprintf("Test%sArtifactService_UserScopeIsolation", name), func(t *testing.T) {
		ctx := t.Context()
		srv, err := factory(t)
		if err != nil {
			t.Fatalf("Failed to set up service: %v", err)
		}
		testArtifactService_UserScopeIsolation(ctx, t, srv, name)
	})
}

func testArtifactService(ctx context.Context, t *testing.T, srv artifact.Service, testSuffix string) {
	appName := "testapp"
	userID := "testuser"
	sessionID := "testsession"

	// Save these artifacts for later subtests.
	testData := []struct {
		fileName string
		version  int64
		artifact *genai.Part
	}{
		// file1.
		{"file1", 1, genai.NewPartFromBytes([]byte("file v1"), "text/plain")},
		{"file1", 2, genai.NewPartFromBytes([]byte("file v2"), "text/plain")},
		{"file1", 3, genai.NewPartFromBytes([]byte("file v3"), "text/plain")},
		// file2.
		{"file2", 1, genai.NewPartFromBytes([]byte("file v3"), "text/plain")},
		// file3.
		{"file3", 1, genai.NewPartFromText("file v1")},
	}

	t.Log("Save file1 and file2")
	for i, data := range testData {
		wantVersion := data.version
		got, err := srv.Save(ctx, &artifact.SaveRequest{
			AppName: appName, UserID: userID, SessionID: sessionID, FileName: data.fileName,
			Part: data.artifact,
		})
		if err != nil || got.Version != wantVersion {
			t.Errorf("[%d] Save() = (%v, %v), want (%v, nil)", i, got.Version, err, wantVersion)
			continue
		}
	}

	t.Run(fmt.Sprintf("Load_%s", testSuffix), func(t *testing.T) {
		fileName := "file1"
		for _, tc := range []struct {
			name    string
			version int64
			want    *genai.Part
		}{
			{"latest", 0, genai.NewPartFromBytes([]byte("file v3"), "text/plain")},
			{"ver=1", 1, genai.NewPartFromBytes([]byte("file v1"), "text/plain")},
			{"ver=2", 2, genai.NewPartFromBytes([]byte("file v2"), "text/plain")},
		} {
			got, err := srv.Load(ctx, &artifact.LoadRequest{
				AppName: appName, UserID: userID, SessionID: sessionID, FileName: fileName,
				Version: tc.version,
			})
			if err != nil || !cmp.Equal(got.Part, tc.want) {
				t.Errorf("Load(%v) = (%v, %v), want (%v, nil)", tc.version, got.Part, err, tc.want)
			}
		}
	})

	t.Run(fmt.Sprintf("List_%s", testSuffix), func(t *testing.T) {
		resp, err := srv.List(ctx, &artifact.ListRequest{
			AppName: appName, UserID: userID, SessionID: sessionID,
		})
		if err != nil {
			t.Fatalf("List() failed: %v", err)
		}
		got := resp.FileNames
		slices.Sort(got)
		want := []string{"file1", "file2", "file3"} // testData has two files.
		if diff := cmp.Diff(got, want); diff != "" {
			t.Errorf("List() = %v, want %v", got, want)
		}
	})

	t.Run(fmt.Sprintf("Versions_%s", testSuffix), func(t *testing.T) {
		resp, err := srv.Versions(ctx, &artifact.VersionsRequest{
			AppName: appName, UserID: userID, SessionID: sessionID, FileName: "file1",
		})
		if err != nil {
			t.Fatalf("Versions() failed: %v", err)
		}
		got := resp.Versions
		slices.Sort(got)
		want := []int64{1, 2, 3}
		if diff := cmp.Diff(got, want); diff != "" {
			t.Errorf("Versions('file1') = %v, want %v", got, want)
		}
	})

	t.Log("Delete file1 version 3")
	if err := srv.Delete(ctx, &artifact.DeleteRequest{
		AppName: appName, UserID: userID, SessionID: sessionID, FileName: "file1",
		Version: 3,
	}); err != nil {
		t.Fatalf("Delete(file1@v3) failed: %v", err)
	}

	t.Run(fmt.Sprintf("LoadAfterDeleteVersion3_%s", testSuffix), func(t *testing.T) {
		resp, err := srv.Load(ctx, &artifact.LoadRequest{
			AppName: appName, UserID: userID, SessionID: sessionID, FileName: "file1",
		})
		if err != nil {
			t.Fatalf("Load('file1') failed: %v", err)
		}
		got := resp.Part
		want := genai.NewPartFromBytes([]byte("file v2"), "text/plain")
		if diff := cmp.Diff(got, want); diff != "" {
			t.Fatalf("Load('file1') = (%v, %v), want (%v, nil)", got, err, want)
		}
	})

	if err := srv.Delete(ctx, &artifact.DeleteRequest{
		AppName: appName, UserID: userID, SessionID: sessionID, FileName: "file1",
	}); err != nil {
		t.Fatalf("Delete(file1) failed: %v", err)
	}

	t.Run(fmt.Sprintf("LoadAfterDelete_%s", testSuffix), func(t *testing.T) {
		got, err := srv.Load(ctx, &artifact.LoadRequest{
			AppName: appName, UserID: userID, SessionID: sessionID, FileName: "file1",
		})
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("Load('file1') = (%v, %v), want error(%v)", got, err, fs.ErrNotExist)
		}
	})

	t.Run(fmt.Sprintf("ListAfterDelete_%s", testSuffix), func(t *testing.T) {
		resp, err := srv.List(ctx, &artifact.ListRequest{
			AppName: appName, UserID: userID, SessionID: sessionID,
		})
		if err != nil {
			t.Fatalf("List() failed: %v", err)
		}
		got := resp.FileNames
		slices.Sort(got)
		want := []string{"file2", "file3"} // testData has two files.
		if diff := cmp.Diff(got, want); diff != "" {
			t.Errorf("List() = %v, want %v", got, want)
		}
	})

	t.Run(fmt.Sprintf("VersionsAfterDelete_%s", testSuffix), func(t *testing.T) {
		got, err := srv.Versions(ctx, &artifact.VersionsRequest{
			AppName: appName, UserID: userID, SessionID: sessionID, FileName: "file1",
		})
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("Versions('file1') = (%v, %v), want error(%v)", got, err, fs.ErrNotExist)
		}
	})

	// Clean up
	if err := srv.Delete(ctx, &artifact.DeleteRequest{
		AppName: appName, UserID: userID, SessionID: sessionID, FileName: "file2",
	}); err != nil {
		t.Fatalf("Delete(file2) failed: %v", err)
	}
	if err := srv.Delete(ctx, &artifact.DeleteRequest{
		AppName: appName, UserID: userID, SessionID: sessionID, FileName: "file3",
	}); err != nil {
		t.Fatalf("Delete(file3) failed: %v", err)
	}
}

func testArtifactService_UserScoped(ctx context.Context, t *testing.T, srv artifact.Service, testSuffix string) {
	appName := "testapp"
	userID := "testuser"
	sessionID := "testsession"

	// Save these artifacts for later subtests.
	testData := []struct {
		fileName string
		version  int64
		artifact *genai.Part
	}{
		// file1.
		{"user:file1", 1, genai.NewPartFromBytes([]byte("file v1"), "text/plain")},
		{"user:file1", 2, genai.NewPartFromBytes([]byte("file v2"), "text/plain")},
		{"user:file1", 3, genai.NewPartFromBytes([]byte("file v3"), "text/plain")},
		// file2.
		{"file2", 1, genai.NewPartFromBytes([]byte("file v3"), "text/plain")},
		// file3.
		{"user:file3", 1, genai.NewPartFromText("file v1")},
	}

	t.Log("Save file1 and file2")
	for i, data := range testData {
		wantVersion := data.version
		got, err := srv.Save(ctx, &artifact.SaveRequest{
			AppName: appName, UserID: userID, SessionID: sessionID, FileName: data.fileName,
			Part: data.artifact,
		})
		if err != nil || got.Version != wantVersion {
			t.Errorf("[%d] Save() = (%v, %v), want (%v, nil)", i, got.Version, err, wantVersion)
			continue
		}
	}

	t.Run(fmt.Sprintf("Load_%s", testSuffix), func(t *testing.T) {
		fileName := "user:file1"
		for _, tc := range []struct {
			name    string
			version int64
			want    *genai.Part
		}{
			{"latest", 0, genai.NewPartFromBytes([]byte("file v3"), "text/plain")},
			{"ver=1", 1, genai.NewPartFromBytes([]byte("file v1"), "text/plain")},
			{"ver=2", 2, genai.NewPartFromBytes([]byte("file v2"), "text/plain")},
		} {
			got, err := srv.Load(ctx, &artifact.LoadRequest{
				AppName: appName, UserID: userID, SessionID: "'user' should be used instead", FileName: fileName,
				Version: tc.version,
			})
			if err != nil || !cmp.Equal(got.Part, tc.want) {
				t.Errorf("Load(%v) = (%v, %v), want (%v, nil)", tc.version, got.Part, err, tc.want)
			}
		}
	})

	t.Run(fmt.Sprintf("List_%s", testSuffix), func(t *testing.T) {
		resp, err := srv.List(ctx, &artifact.ListRequest{
			AppName: appName, UserID: userID, SessionID: sessionID,
		})
		if err != nil {
			t.Fatalf("List() failed: %v", err)
		}
		got := resp.FileNames
		want := []string{"file2", "user:file1", "user:file3"} // testData has two files.
		if diff := cmp.Diff(got, want); diff != "" {
			t.Errorf("List() = %v, want %v", got, want)
		}
	})

	t.Run(fmt.Sprintf("Versions_%s", testSuffix), func(t *testing.T) {
		resp, err := srv.Versions(ctx, &artifact.VersionsRequest{
			AppName: appName, UserID: userID, SessionID: sessionID, FileName: "user:file1",
		})
		if err != nil {
			t.Fatalf("Versions() failed: %v", err)
		}
		got := resp.Versions
		slices.Sort(got)
		want := []int64{1, 2, 3}
		if diff := cmp.Diff(got, want); diff != "" {
			t.Errorf("Versions('user:file1') = %v, want %v", got, want)
		}
	})

	t.Log("Delete user:file1 version 3")
	if err := srv.Delete(ctx, &artifact.DeleteRequest{
		AppName: appName, UserID: userID, SessionID: sessionID, FileName: "user:file1",
		Version: 3,
	}); err != nil {
		t.Fatalf("Delete(user:file1@v3) failed: %v", err)
	}

	t.Run(fmt.Sprintf("LoadAfterDeleteVersion3_%s", testSuffix), func(t *testing.T) {
		resp, err := srv.Load(ctx, &artifact.LoadRequest{
			AppName: appName, UserID: userID, SessionID: sessionID, FileName: "user:file1",
		})
		if err != nil {
			t.Fatalf("Load('user:file1') failed: %v", err)
		}
		got := resp.Part
		want := genai.NewPartFromBytes([]byte("file v2"), "text/plain")
		if diff := cmp.Diff(got, want); diff != "" {
			t.Fatalf("Load('user:file1') = (%v, %v), want (%v, nil)", got, err, want)
		}
	})

	if err := srv.Delete(ctx, &artifact.DeleteRequest{
		AppName: appName, UserID: userID, SessionID: sessionID, FileName: "user:file1",
	}); err != nil {
		t.Fatalf("Delete(user:file1) failed: %v", err)
	}

	t.Run(fmt.Sprintf("LoadAfterDelete_%s", testSuffix), func(t *testing.T) {
		got, err := srv.Load(ctx, &artifact.LoadRequest{
			AppName: appName, UserID: userID, SessionID: sessionID, FileName: "user:file1",
		})
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("Load('user:file1') = (%v, %v), want error(%v)", got, err, fs.ErrNotExist)
		}
	})

	t.Run(fmt.Sprintf("ListAfterDelete_%s", testSuffix), func(t *testing.T) {
		resp, err := srv.List(ctx, &artifact.ListRequest{
			AppName: appName, UserID: userID, SessionID: sessionID,
		})
		if err != nil {
			t.Fatalf("List() failed: %v", err)
		}
		got := resp.FileNames
		slices.Sort(got)
		want := []string{"file2", "user:file3"} // testData has two files.
		if diff := cmp.Diff(got, want); diff != "" {
			t.Errorf("List() = %v, want %v", got, want)
		}
	})

	t.Run(fmt.Sprintf("VersionsAfterDelete_%s", testSuffix), func(t *testing.T) {
		got, err := srv.Versions(ctx, &artifact.VersionsRequest{
			AppName: appName, UserID: userID, SessionID: sessionID, FileName: "user:file1",
		})
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("Versions('user:file1') = (%v, %v), want error(%v)", got, err, fs.ErrNotExist)
		}
	})

	// Clean up
	if err := srv.Delete(ctx, &artifact.DeleteRequest{
		AppName: appName, UserID: userID, SessionID: sessionID, FileName: "file2",
	}); err != nil {
		t.Fatalf("Delete(file2) failed: %v", err)
	}
	if err := srv.Delete(ctx, &artifact.DeleteRequest{
		AppName: appName, UserID: userID, SessionID: sessionID, FileName: "user:file3",
	}); err != nil {
		t.Fatalf("Delete(user:file3) failed: %v", err)
	}
}

// testArtifactService_UserScopeIsolation verifies that an artifact saved in a
// session literally named "user" (which collides with the internal user-scope
// slot) is NOT exposed to other sessions of the same user through List.
//
// Regression test for an isolation bug where the user-scope scan in List would
// return every filename under SessionID="user" — including session-scoped files
// that don't carry the "user:" prefix — leaking them across sessions.
func testArtifactService_UserScopeIsolation(ctx context.Context, t *testing.T, srv artifact.Service, testSuffix string) {
	appName := "testapp"
	userID := "testuser"

	// Save a session-scoped file under a session literally named "user".
	// The filename has no "user:" prefix, so it must remain session-private.
	leakyPart := genai.NewPartFromBytes([]byte("session-private content"), "text/plain")
	if _, err := srv.Save(ctx, &artifact.SaveRequest{
		AppName: appName, UserID: userID, SessionID: "user", FileName: "session_private.txt",
		Part: leakyPart,
	}); err != nil {
		t.Fatalf("Save(sessionID=user, session_private.txt) failed: %v", err)
	}

	// Save a genuine user-scoped artifact (the "user:" prefix routes it to
	// the user-scope slot regardless of the caller's sessionID).
	sharedPart := genai.NewPartFromBytes([]byte("user-shared content"), "text/plain")
	if _, err := srv.Save(ctx, &artifact.SaveRequest{
		AppName: appName, UserID: userID, SessionID: "any_session", FileName: "user:shared.txt",
		Part: sharedPart,
	}); err != nil {
		t.Fatalf("Save(user:shared.txt) failed: %v", err)
	}

	t.Run(fmt.Sprintf("ListFromOtherSession_%s", testSuffix), func(t *testing.T) {
		// A different session must only see the genuine user-scoped artifact,
		// not the session-private file that happened to live in the "user" slot.
		resp, err := srv.List(ctx, &artifact.ListRequest{
			AppName: appName, UserID: userID, SessionID: "other_session",
		})
		if err != nil {
			t.Fatalf("List(other_session) failed: %v", err)
		}
		got := resp.FileNames
		slices.Sort(got)
		want := []string{"user:shared.txt"}
		if diff := cmp.Diff(got, want); diff != "" {
			t.Errorf("List(other_session) leaked session-private artifact: diff (-got +want):\n%s", diff)
		}
	})

	t.Run(fmt.Sprintf("ListFromUserNamedSession_%s", testSuffix), func(t *testing.T) {
		// The session literally named "user" should still see its own
		// session-private file, plus the user-scoped artifact (which shares
		// the same storage slot by design).
		resp, err := srv.List(ctx, &artifact.ListRequest{
			AppName: appName, UserID: userID, SessionID: "user",
		})
		if err != nil {
			t.Fatalf("List(user) failed: %v", err)
		}
		got := resp.FileNames
		slices.Sort(got)
		want := []string{"session_private.txt", "user:shared.txt"}
		if diff := cmp.Diff(got, want); diff != "" {
			t.Errorf("List(sessionID=user) = %v, want %v; diff:\n%s", got, want, diff)
		}
	})

	// Clean up.
	if err := srv.Delete(ctx, &artifact.DeleteRequest{
		AppName: appName, UserID: userID, SessionID: "user", FileName: "session_private.txt",
	}); err != nil {
		t.Fatalf("Delete(session_private.txt) failed: %v", err)
	}
	if err := srv.Delete(ctx, &artifact.DeleteRequest{
		AppName: appName, UserID: userID, SessionID: "any_session", FileName: "user:shared.txt",
	}); err != nil {
		t.Fatalf("Delete(user:shared.txt) failed: %v", err)
	}
}

func testArtifactService_Empty(ctx context.Context, t *testing.T, srv artifact.Service, testSuffix string) {
	t.Run(fmt.Sprintf("Load_%s", testSuffix), func(t *testing.T) {
		got, err := srv.Load(ctx, &artifact.LoadRequest{
			AppName: "app", UserID: "user", SessionID: "session", FileName: "file",
		})
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("List() = (%v, %v), want error(%v)", got, err, fs.ErrNotExist)
		}
	})
	t.Run(fmt.Sprintf("List_%s", testSuffix), func(t *testing.T) {
		_, err := srv.List(ctx, &artifact.ListRequest{
			AppName: "app", UserID: "user", SessionID: "session",
		})
		if err != nil {
			t.Fatalf("List() failed: %v", err)
		}
	})
	t.Run(fmt.Sprintf("Delete_%s", testSuffix), func(t *testing.T) {
		err := srv.Delete(ctx, &artifact.DeleteRequest{
			AppName: "app", UserID: "user", SessionID: "session", FileName: "file1",
		})
		if err != nil {
			t.Fatalf("Delete() failed: %v", err)
		}
	})
	t.Run(fmt.Sprintf("Versions_%s", testSuffix), func(t *testing.T) {
		got, err := srv.Versions(ctx, &artifact.VersionsRequest{
			AppName: "app", UserID: "user", SessionID: "session", FileName: "file1",
		})
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("Versions() = (%v, %v), want error(%v)", got, err, fs.ErrNotExist)
		}
	})
}
