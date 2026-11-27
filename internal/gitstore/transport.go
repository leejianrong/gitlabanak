package gitstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
)

const (
	UploadPack  = "git-upload-pack"
	ReceivePack = "git-receive-pack"
)

// transport serves this store's repository through go-git's server, which has no HTTP layer of
// its own. Callers hold s.mu: go-git reads and writes the storage lazily.
func (s *Store) sessionEndpoint() (transport.Transport, *transport.Endpoint, error) {
	ep, err := transport.NewEndpoint("/repo")
	if err != nil {
		return nil, nil, err
	}
	return server.NewServer(server.MapLoader{ep.String(): s.repo.Storer}), ep, nil
}

// Advertise writes the body of GET info/refs?service=<service> for smart HTTP.
func (s *Store) Advertise(service string, w io.Writer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ep, err := s.sessionEndpoint()
	if err != nil {
		return err
	}
	var ar *packp.AdvRefs
	switch service {
	case UploadPack:
		sess, err := t.NewUploadPackSession(ep, nil)
		if err != nil {
			return err
		}
		ar, err = sess.AdvertisedReferences()
		if err != nil {
			return err
		}
	case ReceivePack:
		sess, err := t.NewReceivePackSession(ep, nil)
		if err != nil {
			return err
		}
		ar, err = sess.AdvertisedReferences()
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown service %q", service)
	}
	ar.Prefix = [][]byte{[]byte("# service=" + service), pktline.Flush}
	return ar.Encode(w)
}

// Serve answers POST <service> for smart HTTP: it reads the request from r and writes the
// response to w. The response is built in memory so no repository access outlives the lock.
func (s *Store) Serve(ctx context.Context, service string, r io.Reader, w io.Writer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ep, err := s.sessionEndpoint()
	if err != nil {
		return err
	}
	var out bytes.Buffer
	switch service {
	case UploadPack:
		err = s.uploadPack(ctx, t, ep, r, &out)
	case ReceivePack:
		err = s.receivePack(ctx, t, ep, r, &out)
	default:
		err = fmt.Errorf("unknown service %q", service)
	}
	if err != nil {
		return err
	}
	_, err = w.Write(out.Bytes())
	return err
}

func (s *Store) uploadPack(ctx context.Context, t transport.Transport, ep *transport.Endpoint, r io.Reader, w io.Writer) error {
	req := packp.NewUploadPackRequest()
	if err := req.Decode(r); err != nil {
		return err
	}
	// The haves follow the wants. Without "done" the client is still negotiating.
	done := false
	sc := pktline.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(string(sc.Bytes()))
		switch {
		case line == "done":
			done = true
		case strings.HasPrefix(line, "have "):
			req.Haves = append(req.Haves, plumbing.NewHash(strings.TrimPrefix(line, "have ")))
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if !done && len(req.Haves) > 0 {
		return pktline.NewEncoder(w).EncodeString("NAK\n")
	}
	sess, err := t.NewUploadPackSession(ep, nil)
	if err != nil {
		return err
	}
	if _, err := sess.AdvertisedReferences(); err != nil {
		return err
	}
	resp, err := sess.UploadPack(ctx, req)
	if err != nil {
		return err
	}
	return resp.Encode(w)
}

func (s *Store) receivePack(ctx context.Context, t transport.Transport, ep *transport.Endpoint, r io.Reader, w io.Writer) error {
	req := packp.NewReferenceUpdateRequest()
	if err := req.Decode(r); err != nil {
		return err
	}
	// A push that only deletes refs carries no packfile, but go-git still tries to read one.
	onlyDeletes := len(req.Commands) > 0
	for _, c := range req.Commands {
		onlyDeletes = onlyDeletes && c.New.IsZero()
	}
	if onlyDeletes {
		req.Packfile = nil
	}
	sess, err := t.NewReceivePackSession(ep, nil)
	if err != nil {
		return err
	}
	if _, err := sess.AdvertisedReferences(); err != nil {
		return err
	}
	status, err := sess.ReceivePack(ctx, req)
	if status != nil && req.Capabilities.Supports("report-status") {
		_ = status.Encode(w)
	}
	return err
}
