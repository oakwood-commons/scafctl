// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gitcredential

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// MaxInputSize caps total bytes read from a single git-credential stdin request (1 MiB).
	MaxInputSize = 1 << 20

	// MaxLineLength caps the byte length of any single attribute line read from stdin.
	MaxLineLength = 65535

	// MaxAttributes caps the total number of attribute lines accepted in one request.
	MaxAttributes = 256

	// MaxRepeatedAttributeItems caps slice-valued protocol fields (wwwauth[], capability[]).
	MaxRepeatedAttributeItems = 32

	// MaxHostLength caps the byte length of a Git host or host:port attribute.
	MaxHostLength = 253

	// MaxRepoPathLength caps the byte length of a normalized repository path.
	MaxRepoPathLength = 1024

	// RedactedAttribute is emitted in String and GoString representations for secret fields.
	RedactedAttribute = "[REDACTED]"
)

// Request represents parsed attributes from git-credential stdin.
type Request struct {
	Protocol          string     `json:"protocol,omitempty" yaml:"protocol,omitempty" doc:"URL scheme (https or loopback http)" maxLength:"16" example:"https"`
	Host              string     `json:"host,omitempty" yaml:"host,omitempty" doc:"Target Git host or host:port" maxLength:"253" example:"github.com"`
	Path              string     `json:"path,omitempty" yaml:"path,omitempty" doc:"Repository path without leading slash" maxLength:"1024" example:"acme-corp/repo.git"`
	Username          string     `json:"username,omitempty" yaml:"username,omitempty" doc:"Requested or stored username" maxLength:"256" example:"x-access-token"`
	HasUsername       bool       `json:"-" yaml:"-" doc:"Whether username was explicitly supplied (including empty username= or URL userinfo)"`
	Password          string     `json:"-" yaml:"-" doc:"Secret password or token"`     //nolint:gosec // Git credential protocol field
	OAuthRefreshToken string     `json:"-" yaml:"-" doc:"Optional OAuth refresh token"` //nolint:gosec // Git credential protocol field
	PasswordExpiryUTC *time.Time `json:"passwordExpiryUtc,omitempty" yaml:"passwordExpiryUtc,omitempty" doc:"Optional UTC token expiration timestamp"`
	WWWAuth           []string   `json:"wwwauth,omitempty" yaml:"wwwauth,omitempty" doc:"WWW-Authenticate headers reported by Git" maxItems:"32"`
	Capabilities      []string   `json:"capabilities,omitempty" yaml:"capabilities,omitempty" doc:"Protocol capabilities advertised by Git" maxItems:"32"`
}

// String returns a redacted summary of Request safe for logging and diagnostics.
func (r Request) String() string {
	pw := ""
	if r.Password != "" {
		pw = RedactedAttribute
	}
	rt := ""
	if r.OAuthRefreshToken != "" {
		rt = RedactedAttribute
	}
	return fmt.Sprintf("Request{Protocol:%q, Host:%q, Path:%q, Username:%q, Password:%q, OAuthRefreshToken:%q}",
		r.Protocol, r.Host, r.Path, r.Username, pw, rt)
}

// GoString returns a redacted Go-syntax representation so %#v never leaks credentials.
func (r Request) GoString() string {
	return r.String()
}

// EffectiveHasUsername reports whether a username attribute (including an
// explicit empty "username=" or URL userinfo) was supplied on the Request.
func (r *Request) EffectiveHasUsername() bool {
	return r != nil && (r.HasUsername || r.Username != "")
}

// Response represents attributes written to git-credential stdout.
type Response struct {
	Protocol          string     `json:"protocol,omitempty" yaml:"protocol,omitempty" doc:"URL scheme" maxLength:"16" example:"https"`
	Host              string     `json:"host,omitempty" yaml:"host,omitempty" doc:"Target Git host or host:port" maxLength:"253" example:"github.com"`
	Path              string     `json:"path,omitempty" yaml:"path,omitempty" doc:"Repository path without leading slash" maxLength:"1024" example:"acme-corp/repo.git"`
	Username          string     `json:"username,omitempty" yaml:"username,omitempty" doc:"Resolved Git HTTP Basic username" maxLength:"256" example:"x-access-token"`
	Password          string     `json:"-" yaml:"-" doc:"Resolved secret password or token"` //nolint:gosec // Git credential protocol field
	OAuthRefreshToken string     `json:"-" yaml:"-" doc:"Optional OAuth refresh token"`      //nolint:gosec // Git credential protocol field
	PasswordExpiryUTC *time.Time `json:"passwordExpiryUtc,omitempty" yaml:"passwordExpiryUtc,omitempty" doc:"Optional UTC token expiration timestamp"`
	Quit              bool       `json:"quit,omitempty" yaml:"quit,omitempty" doc:"Instruct Git to stop querying further credential helpers"`
}

// String returns a redacted summary of Response safe for logging and diagnostics.
func (r Response) String() string {
	pw := ""
	if r.Password != "" {
		pw = RedactedAttribute
	}
	rt := ""
	if r.OAuthRefreshToken != "" {
		rt = RedactedAttribute
	}
	return fmt.Sprintf("Response{Protocol:%q, Host:%q, Path:%q, Username:%q, Password:%q, OAuthRefreshToken:%q, Quit:%t}",
		r.Protocol, r.Host, r.Path, r.Username, pw, rt, r.Quit)
}

// GoString returns a redacted Go-syntax representation so %#v never leaks credentials.
func (r Response) GoString() string {
	return r.String()
}

// ParseRequest reads git-credential key=value lines from r, stopping immediately
// at the first blank line or EOF so an open pipe from git never deadlocks.
func ParseRequest(r io.Reader) (*Request, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: nil reader", ErrInvalidProtocolInput)
	}

	br := bufio.NewReaderSize(r, 4096)
	req := &Request{}
	var totalBytes int
	var attrCount int

	for {
		lineBytes, totalDelta, eof, err := readBoundedProtocolLine(br, MaxLineLength, MaxInputSize-totalBytes)
		totalBytes += totalDelta
		if err != nil {
			return nil, err
		}

		// Strip trailing \r\n or \n.
		lineBytes = bytes.TrimSuffix(lineBytes, []byte{'\n'})
		lineBytes = bytes.TrimSuffix(lineBytes, []byte{'\r'})

		// First blank line terminates the credential message.
		if len(lineBytes) == 0 {
			break
		}

		attrCount++
		if attrCount > MaxAttributes {
			return nil, fmt.Errorf("%w: request exceeds maximum of %d attributes", ErrInvalidProtocolInput, MaxAttributes)
		}

		if bytes.IndexByte(lineBytes, 0) >= 0 || bytes.IndexByte(lineBytes, '\r') >= 0 || bytes.IndexByte(lineBytes, '\n') >= 0 {
			return nil, fmt.Errorf("%w: attribute contains control byte", ErrInvalidProtocolInput)
		}

		line := string(lineBytes)
		key, val, ok := strings.Cut(line, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("%w: malformed attribute line", ErrInvalidProtocolInput)
		}

		if err := applyRequestAttribute(req, key, val); err != nil {
			return nil, err
		}

		if eof {
			break
		}
	}

	return req, nil
}

func readBoundedProtocolLine(br *bufio.Reader, maxLine, remainingTotal int) ([]byte, int, bool, error) {
	var buf []byte
	readCount := 0

	for {
		frag, err := br.ReadSlice('\n')
		readCount += len(frag)
		if readCount > remainingTotal {
			return nil, readCount, false, fmt.Errorf("%w: input exceeds maximum size of %d bytes", ErrInvalidProtocolInput, MaxInputSize)
		}
		buf = append(buf, frag...)

		// Compute content length excluding trailing \r\n or \n before checking maxLine.
		contentLen := len(buf)
		if contentLen > 0 && buf[contentLen-1] == '\n' {
			contentLen--
		}
		if contentLen > 0 && buf[contentLen-1] == '\r' {
			contentLen--
		}
		if contentLen > maxLine {
			return nil, readCount, false, fmt.Errorf("%w: attribute line exceeds maximum length of %d bytes", ErrInvalidProtocolInput, MaxLineLength)
		}

		if err == nil {
			return buf, readCount, false, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			return buf, readCount, true, nil
		}
		return nil, readCount, false, fmt.Errorf("read git credential input: %w", err)
	}
}

func applyRequestAttribute(req *Request, key, val string) error {
	switch key {
	case "url":
		if strings.ContainsRune(val, '\\') {
			return fmt.Errorf("%w: url attribute must not contain backslashes", ErrInvalidProtocolInput)
		}
		u, err := url.Parse(val)
		if err != nil || u.Scheme == "" || u.Host == "" || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("%w: invalid url attribute", ErrInvalidProtocolInput)
		}
		for _, part := range []string{u.Scheme, u.Host, u.Path} {
			if err := validateProtocolValue(part); err != nil {
				return err
			}
		}
		trimmedHost := strings.TrimSpace(u.Host)
		if err := ValidateHostAttribute(trimmedHost); err != nil {
			return err
		}
		if err := ValidateRepoPath(u.EscapedPath()); err != nil {
			return err
		}
		if err := ValidateRepoPath(u.Path); err != nil {
			return err
		}
		// Match Git's credential_from_url_gently: url= resets path, username,
		// and password from the parsed URL so prior lines never bleed across.
		req.Protocol = strings.ToLower(strings.TrimSpace(u.Scheme))
		req.Host = trimmedHost
		req.Path = strings.TrimPrefix(u.Path, "/")
		req.Username = ""
		req.HasUsername = false
		req.Password = ""
		if u.User != nil {
			uname := u.User.Username()
			if err := validateProtocolValue(uname); err != nil {
				return err
			}
			req.Username = uname
			req.HasUsername = true
			if pw, hasPW := u.User.Password(); hasPW {
				if err := validateProtocolValue(pw); err != nil {
					return err
				}
				req.Password = pw
			}
		}
	case "protocol":
		req.Protocol = strings.ToLower(strings.TrimSpace(val))
	case "host":
		trimmed := strings.TrimSpace(val)
		if trimmed != "" {
			if err := ValidateHostAttribute(trimmed); err != nil {
				return err
			}
		}
		req.Host = trimmed
	case "path":
		if err := ValidateRepoPath(val); err != nil {
			return err
		}
		req.Path = strings.TrimPrefix(val, "/")
	case "username":
		req.Username = val
		req.HasUsername = true
	case "password":
		req.Password = val
	case "oauth_refresh_token":
		req.OAuthRefreshToken = val
	case "password_expiry_utc":
		if val != "" {
			sec, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return fmt.Errorf("%w: invalid password_expiry_utc %q", ErrInvalidProtocolInput, val)
			}
			exp := time.Unix(sec, 0).UTC()
			req.PasswordExpiryUTC = &exp
		}
	case "wwwauth[]":
		if len(req.WWWAuth) >= MaxRepeatedAttributeItems {
			return fmt.Errorf("%w: wwwauth[] exceeds maximum of %d entries", ErrInvalidProtocolInput, MaxRepeatedAttributeItems)
		}
		req.WWWAuth = append(req.WWWAuth, val)
	case "capability[]":
		if len(req.Capabilities) >= MaxRepeatedAttributeItems {
			return fmt.Errorf("%w: capability[] exceeds maximum of %d entries", ErrInvalidProtocolInput, MaxRepeatedAttributeItems)
		}
		req.Capabilities = append(req.Capabilities, val)
	default:
		// Git's credential protocol specifies that unrecognized attributes are ignored.
	}
	return nil
}

// WriteResponse serializes resp in Git's key=value wire format terminated by a
// blank line, rejecting any attribute value containing CR, LF, or NUL bytes.
func WriteResponse(w io.Writer, resp *Response) error {
	if w == nil {
		return fmt.Errorf("%w: nil writer", ErrInvalidProtocolInput)
	}
	if resp == nil {
		return nil
	}

	type kv struct {
		key string
		val string
	}
	var pairs []kv
	addIfNonEmpty := func(key, val string) error {
		if val == "" {
			return nil
		}
		if err := validateProtocolValue(val); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		pairs = append(pairs, kv{key: key, val: val})
		return nil
	}

	if err := addIfNonEmpty("protocol", resp.Protocol); err != nil {
		return err
	}
	if err := addIfNonEmpty("host", resp.Host); err != nil {
		return err
	}
	if err := addIfNonEmpty("path", strings.TrimPrefix(resp.Path, "/")); err != nil {
		return err
	}
	if resp.Username != "" || resp.Password != "" {
		if err := validateProtocolValue(resp.Username); err != nil {
			return fmt.Errorf("username: %w", err)
		}
		pairs = append(pairs, kv{key: "username", val: resp.Username})
	}
	if err := addIfNonEmpty("password", resp.Password); err != nil {
		return err
	}
	if resp.PasswordExpiryUTC != nil && resp.PasswordExpiryUTC.After(time.Now()) {
		pairs = append(pairs, kv{
			key: "password_expiry_utc",
			val: strconv.FormatInt(resp.PasswordExpiryUTC.UTC().Unix(), 10),
		})
	}
	if err := addIfNonEmpty("oauth_refresh_token", resp.OAuthRefreshToken); err != nil {
		return err
	}
	if resp.Quit {
		pairs = append(pairs, kv{key: "quit", val: "1"})
	}

	var buf bytes.Buffer
	for _, p := range pairs {
		buf.WriteString(p.key)
		buf.WriteByte('=')
		buf.WriteString(p.val)
		buf.WriteByte('\n')
	}
	buf.WriteByte('\n')

	_, err := w.Write(buf.Bytes())
	return err
}

func validateProtocolValue(val string) error {
	if strings.ContainsAny(val, "\r\n\x00") {
		return fmt.Errorf("%w: attribute value must not contain CR, LF, or NUL", ErrInvalidProtocolInput)
	}
	return nil
}

// ValidateHostAttribute verifies that a host or host:port value does not exceed
// MaxHostLength or contain URL delimiters, userinfo separators, whitespace, or control bytes.
func ValidateHostAttribute(host string) error {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return ErrMissingHost
	}
	if len(trimmed) > MaxHostLength {
		return fmt.Errorf("%w: host exceeds maximum length of %d bytes", ErrInvalidProtocolInput, MaxHostLength)
	}
	if strings.ContainsAny(trimmed, "/?#@\\ \t\r\n\x00") {
		return fmt.Errorf("%w: invalid host %q", ErrInvalidProtocolInput, host)
	}
	return nil
}

// CleanRepoPath trims leading and trailing slashes while preserving path
// whitespace and any ".git" suffix so distinct repository endpoints (e.g.
// "/team/repo" vs "/team/repo%20" or "/team/repo.git") retain distinct identities.
func CleanRepoPath(raw string) string {
	return strings.Trim(raw, "/")
}

// ValidateRepoPath enforces MaxRepoPathLength and rejects path-traversal dot
// segments ("." / ".."), empty double-slash segments ("//"), backslashes,
// query/fragment delimiters, control bytes, and percent-encoded separator or
// dot-segment bypasses ("%2e", "%2f", "%5c").
func ValidateRepoPath(raw string) error {
	if raw == "" || raw == "/" {
		return nil
	}
	if strings.Contains(raw, "//") {
		return fmt.Errorf("%w: repository path %q must not contain empty '//' segments", ErrInvalidProtocolInput, raw)
	}
	if len(strings.TrimPrefix(raw, "/")) > MaxRepoPathLength {
		return fmt.Errorf("%w: repository path exceeds maximum length of %d bytes", ErrInvalidProtocolInput, MaxRepoPathLength)
	}
	if strings.ContainsAny(raw, "\\?#\r\n\x00") {
		return fmt.Errorf("%w: repository path %q contains invalid characters", ErrInvalidProtocolInput, raw)
	}
	lowerRaw := strings.ToLower(raw)
	if strings.Contains(lowerRaw, "%2f") || strings.Contains(lowerRaw, "%5c") || strings.Contains(lowerRaw, "%00") {
		return fmt.Errorf("%w: repository path %q must not contain encoded path separators or NUL bytes", ErrInvalidProtocolInput, raw)
	}
	if err := checkCleanPathSegments(raw, raw); err != nil {
		return err
	}
	if strings.ContainsRune(raw, '%') {
		unescaped, err := url.PathUnescape(raw)
		if err != nil {
			return fmt.Errorf("%w: repository path %q has malformed percent-encoding", ErrInvalidProtocolInput, raw)
		}
		if strings.Contains(unescaped, "//") {
			return fmt.Errorf("%w: repository path %q must not contain empty '//' segments", ErrInvalidProtocolInput, raw)
		}
		if len(strings.TrimPrefix(unescaped, "/")) > MaxRepoPathLength {
			return fmt.Errorf("%w: repository path exceeds maximum length of %d bytes", ErrInvalidProtocolInput, MaxRepoPathLength)
		}
		if strings.ContainsAny(unescaped, "\\?#\r\n\x00") {
			return fmt.Errorf("%w: repository path %q contains invalid encoded characters", ErrInvalidProtocolInput, raw)
		}
		if err := checkCleanPathSegments(unescaped, raw); err != nil {
			return err
		}
	}
	return nil
}

func checkCleanPathSegments(p, rawForErr string) error {
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return nil
	}
	for _, seg := range strings.Split(trimmed, "/") {
		if seg == "" {
			return fmt.Errorf("%w: repository path %q must not contain empty '//' segments", ErrInvalidProtocolInput, rawForErr)
		}
		if seg == "." || seg == ".." {
			return fmt.Errorf("%w: repository path %q must not contain traversal dot segments", ErrInvalidProtocolInput, rawForErr)
		}
	}
	return nil
}

// NormalizeHostForProtocol lowercases host and strips explicit default ports
// (:443 for https or empty protocol, :80 for http) while preserving non-default ports.
func NormalizeHostForProtocol(protocol, host string) string {
	normHost := strings.ToLower(strings.TrimSpace(host))
	if normHost == "" {
		return ""
	}
	proto := strings.ToLower(strings.TrimSpace(protocol))
	var defaultPort string
	switch proto {
	case "", "https":
		defaultPort = "443"
	case "http":
		defaultPort = "80"
	default:
		return normHost
	}
	return stripMatchingPort(normHost, defaultPort)
}

func stripMatchingPort(normHost, targetPort string) string {
	if strings.HasPrefix(normHost, "[") {
		if h, p, err := net.SplitHostPort(normHost); err == nil && p == targetPort {
			return "[" + h + "]"
		}
		return normHost
	}
	if strings.Count(normHost, ":") == 1 {
		if h, p, err := net.SplitHostPort(normHost); err == nil && p == targetPort {
			return h
		}
	}
	return normHost
}

// ValidateTransport verifies that the requested protocol is HTTPS, or plain HTTP
// restricted to loopback hosts unless allowInsecureHTTP is explicitly enabled.
func ValidateTransport(protocol, host string, allowInsecureHTTP bool) error {
	proto := strings.ToLower(strings.TrimSpace(protocol))
	switch proto {
	case "https":
		return nil
	case "http":
		if allowInsecureHTTP || IsLoopbackHost(host) {
			return nil
		}
		return fmt.Errorf("%w: %s://%s", ErrInsecureTransport, proto, host)
	default:
		return fmt.Errorf("%w: unsupported protocol %q (expected https or loopback http)", ErrInvalidProtocolInput, protocol)
	}
}

// ExtractHostname strips optional port and IPv6 brackets from a host or host:port value.
func ExtractHostname(host string) string {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "[") {
		if h, _, err := net.SplitHostPort(trimmed); err == nil {
			return strings.ToLower(h)
		}
		if strings.HasSuffix(trimmed, "]") {
			return strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]"))
		}
	}
	if strings.Count(trimmed, ":") == 1 {
		if h, _, err := net.SplitHostPort(trimmed); err == nil {
			return strings.ToLower(h)
		}
	}
	return strings.ToLower(trimmed)
}

// IsLoopbackHost reports whether host refers to localhost, 127.0.0.1, or ::1
// (with an optional port).
func IsLoopbackHost(host string) bool {
	switch ExtractHostname(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

// CanonicalURL builds a normalized "<protocol>://<host>[/<repoPath>]" identifier
// after validating host and repository path safety and stripping the protocol's
// default port while preserving the repository path (including any ".git" suffix).
func CanonicalURL(protocol, host, repoPath string) (string, error) {
	proto := strings.ToLower(strings.TrimSpace(protocol))
	if proto == "" {
		return "", fmt.Errorf("%w: missing protocol", ErrInvalidProtocolInput)
	}
	if err := ValidateHostAttribute(host); err != nil {
		return "", err
	}
	if err := ValidateRepoPath(repoPath); err != nil {
		return "", err
	}
	normHost := NormalizeHostForProtocol(proto, host)
	cleanPath := CleanRepoPath(repoPath)
	if cleanPath == "" {
		return proto + "://" + normHost, nil
	}
	return proto + "://" + normHost + "/" + cleanPath, nil
}
