package appleconnect

import (
	"bytes"
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAuthBaseURL = "https://idmsa.apple.com/appleauth/auth"
	defaultASCBaseURL  = "https://appstoreconnect.apple.com"
	clientSecretBytes  = 256
	derivedPasswordLen = 32
	appleSRPModulusHex = "AC6BDB41324A9A9BF166DE5E1389582FAF72B6651987EE07FC319294" +
		"3DB56050A37329CBB4A099ED8193E0757767A13DD52312AB4B03310D" +
		"CD7F48A9DA04FD50E8083969EDB767B0CF6095179A163AB3661A05FB" +
		"D5FAAAE82918A9962F0B93B855F97993EC975EEAA80D740ADBF4FF74" +
		"7359D041D5C33EA71D281E446B14773BCA97B43A23FB801676BD207A" +
		"436C6481F1D2B9078717461A5B9D32E688F87748544523B524B0D57D" +
		"5EA77A2775D2ECFA032CFBDBF52FB3786160279004E57AE6AF874E73" +
		"03CE53299CCC041C7BC308D82A5698F3A8D0C38271AE35F8E9DBFBB6" +
		"94B5C803D89F7AE435DE236D525F54759B65E372FCD68EF20FA7111F" +
		"9E4AFF73"
)

var (
	ErrInvalidCredentials = errors.New("incorrect Apple Account email or password")
	ErrAccountAction      = errors.New("complete the pending Apple Account prompt in a browser and try again")
)

type Options struct {
	HTTPClient  *http.Client
	AuthBaseURL string
	ASCBaseURL  string
}

type Client struct {
	httpClient  *http.Client
	authBaseURL string
	ascBaseURL  string
}

type Provider struct {
	ID       int64  `json:"provider_id"`
	PublicID string `json:"public_provider_id,omitempty"`
	Name     string `json:"name"`
}

type Session struct {
	client           *Client
	ServiceKey       string
	AppleIDSessionID string
	SCNT             string
	Email            string
	Provider         Provider
	Providers        []Provider
	factorMethod     string
	phoneID          int
	phoneMode        string
	destination      string
}

type TwoFactorRequiredError struct{}

func (*TwoFactorRequiredError) Error() string { return "two-factor authentication required" }

type Challenge struct {
	Method      string `json:"method"`
	Destination string `json:"destination,omitempty"`
	CodeLength  int    `json:"code_length"`
}

type authOptions struct {
	NoTrustedDevices    bool `json:"noTrustedDevices"`
	TrustedPhoneNumbers []struct {
		ID                 int    `json:"id"`
		PushMode           string `json:"pushMode"`
		NumberWithDialCode string `json:"numberWithDialCode"`
	} `json:"trustedPhoneNumbers"`
	SecurityCode struct {
		Length int `json:"length"`
	} `json:"securityCode"`
}

type signinInitResponse struct {
	Iteration  int             `json:"iteration"`
	Salt       string          `json:"salt"`
	Protocol   string          `json:"protocol"`
	ServerPubB string          `json:"b"`
	Challenge  json.RawMessage `json:"c"`
}

func New(opts Options) (*Client, error) {
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if httpClient.Jar == nil {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return nil, err
		}
		httpClient.Jar = jar
	}
	clientCopy := *httpClient
	transport := httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	clientCopy.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		requestCopy := req.Clone(req.Context())
		requestCopy.Header = req.Header.Clone()
		quoteDESCookies(requestCopy.Header)
		return transport.RoundTrip(requestCopy)
	})
	httpClient = &clientCopy
	authBaseURL := strings.TrimRight(opts.AuthBaseURL, "/")
	if authBaseURL == "" {
		authBaseURL = defaultAuthBaseURL
	}
	ascBaseURL := strings.TrimRight(opts.ASCBaseURL, "/")
	if ascBaseURL == "" {
		ascBaseURL = defaultASCBaseURL
	}
	return &Client{httpClient: httpClient, authBaseURL: authBaseURL, ascBaseURL: ascBaseURL}, nil
}

func (c *Client) Login(ctx context.Context, email, password string) (*Session, error) {
	if strings.TrimSpace(email) == "" {
		return nil, errors.New("an Apple Account email is required")
	}
	if password == "" {
		return nil, errors.New("an Apple Account password is required")
	}
	serviceKey, err := c.authServiceKey(ctx)
	if err != nil {
		return nil, err
	}
	session := &Session{client: c, ServiceKey: serviceKey, Email: strings.TrimSpace(email)}
	if err := c.performSRPLogin(ctx, session.Email, password, serviceKey); err != nil {
		var twoFactor *twoFactorStateError
		if errors.As(err, &twoFactor) {
			session.AppleIDSessionID = twoFactor.sessionID
			session.SCNT = twoFactor.scnt
			return session, &TwoFactorRequiredError{}
		}
		return nil, err
	}
	if err := c.populateSession(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

func (c *Client) PrepareTwoFactor(ctx context.Context, session *Session, useSMS bool, phoneNumber string) (*Challenge, error) {
	if err := validateContinuation(session); err != nil {
		return nil, err
	}
	var options authOptions
	if err := c.authRequest(ctx, session, http.MethodGet, "", nil, &options); err != nil {
		return nil, err
	}
	length := options.SecurityCode.Length
	if length == 0 {
		length = 6
	}
	if !useSMS && !options.NoTrustedDevices {
		session.factorMethod = "trusteddevice"
		return &Challenge{Method: "trusted_device", CodeLength: length}, nil
	}
	if len(options.TrustedPhoneNumbers) == 0 {
		return nil, errors.New("the Apple Account has no trusted phone numbers")
	}
	phone, err := selectPhone(options, phoneNumber)
	if err != nil {
		return nil, err
	}
	mode := phone.PushMode
	if mode == "" {
		mode = "sms"
	}
	session.factorMethod = "phone"
	session.phoneID = phone.ID
	session.phoneMode = mode
	session.destination = phone.NumberWithDialCode
	if !(options.NoTrustedDevices && len(options.TrustedPhoneNumbers) == 1) {
		body := map[string]any{"phoneNumber": map[string]int{"id": phone.ID}, "mode": mode}
		if err := c.authRequest(ctx, session, http.MethodPut, "/verify/phone", body, nil); err != nil {
			return nil, err
		}
	}
	return &Challenge{Method: mode, Destination: phone.NumberWithDialCode, CodeLength: length}, nil
}

func (c *Client) CompleteTwoFactor(ctx context.Context, session *Session, code string) error {
	if err := validateContinuation(session); err != nil {
		return err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return errors.New("an Apple verification code is required")
	}
	var path string
	var body any
	switch session.factorMethod {
	case "trusteddevice":
		path = "/verify/trusteddevice/securitycode"
		body = map[string]any{"securityCode": map[string]string{"code": code}}
	case "phone":
		path = "/verify/phone/securitycode"
		body = map[string]any{
			"securityCode": map[string]string{"code": code},
			"phoneNumber":  map[string]int{"id": session.phoneID},
			"mode":         session.phoneMode,
		}
	default:
		return errors.New("the Apple verification method has not been prepared")
	}
	if err := c.authRequest(ctx, session, http.MethodPost, path, body, nil); err != nil {
		return err
	}
	if err := c.authRequest(ctx, session, http.MethodGet, "/2sv/trust", nil, nil); err != nil {
		return err
	}
	return c.populateSession(ctx, session)
}

func (c *Client) SelectProvider(ctx context.Context, session *Session, providerID int64) error {
	if providerID == 0 || session.Provider.ID == providerID {
		return nil
	}
	found := false
	for _, provider := range session.Providers {
		if provider.ID == providerID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("the Apple provider %d is not available for this account", providerID)
	}
	body := map[string]any{"provider": map[string]int64{"providerId": providerID}}
	if err := c.doJSON(ctx, http.MethodPost, c.ascBaseURL+"/olympus/v1/session", body, nil, http.Header{"X-Requested-With": []string{"olympus-ui"}}); err != nil {
		return err
	}
	if err := c.populateSession(ctx, session); err != nil {
		return err
	}
	if session.Provider.ID != providerID {
		return fmt.Errorf("the Apple provider selection did not take effect: selected %d but session reports %d", providerID, session.Provider.ID)
	}
	return nil
}

func validateContinuation(session *Session) error {
	if session == nil || session.client == nil {
		return errors.New("an Apple session is required")
	}
	if session.ServiceKey == "" || session.AppleIDSessionID == "" || session.SCNT == "" {
		return errors.New("the Apple session is missing two-factor continuation state")
	}
	return nil
}

func selectPhone(options authOptions, requested string) (struct {
	ID                 int    `json:"id"`
	PushMode           string `json:"pushMode"`
	NumberWithDialCode string `json:"numberWithDialCode"`
}, error) {
	if requested == "" {
		return options.TrustedPhoneNumbers[0], nil
	}
	for _, phone := range options.TrustedPhoneNumbers {
		if phone.NumberWithDialCode == requested || phoneMatches(requested, phone.NumberWithDialCode) {
			return phone, nil
		}
	}
	return options.TrustedPhoneNumbers[0], fmt.Errorf("trusted phone number %q was not found", requested)
}

func phoneMatches(number, masked string) bool {
	clean := strings.NewReplacer(" ", "", "\u00a0", "", "-", "", "(", "", ")", "", "\"", "").Replace
	number = clean(number)
	masked = clean(masked)
	bullets := strings.Count(masked, "•")
	if bullets == 0 {
		return number == masked
	}
	first := strings.Index(masked, "•")
	last := strings.LastIndex(masked, "•")
	prefix := regexp.QuoteMeta(masked[:first])
	suffix := regexp.QuoteMeta(masked[last+len("•"):])
	minimum := bullets - 2
	if minimum < 1 {
		minimum = 1
	}
	matched, _ := regexp.MatchString("^"+prefix+"[0-9]{"+strconv.Itoa(minimum)+","+strconv.Itoa(bullets)+"}"+suffix+"$", number)
	return matched
}

func (c *Client) authServiceKey(ctx context.Context) (string, error) {
	var payload struct {
		AuthServiceKey string `json:"authServiceKey"`
		ServiceKey     string `json:"serviceKey"`
	}
	endpoint := c.ascBaseURL + "/olympus/v1/app/config?hostname=itunesconnect.apple.com"
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &payload, nil); err != nil {
		return "", fmt.Errorf("get Apple auth service key: %w", err)
	}
	key := strings.TrimSpace(payload.AuthServiceKey)
	if key == "" {
		key = strings.TrimSpace(payload.ServiceKey)
	}
	if key == "" {
		return "", errors.New("the Apple auth service key is empty")
	}
	return key, nil
}

func (c *Client) populateSession(ctx context.Context, session *Session) error {
	var payload struct {
		Provider struct {
			ProviderID       int64  `json:"providerId"`
			PublicProviderID string `json:"publicProviderId"`
			Name             string `json:"name"`
		} `json:"provider"`
		AvailableProviders []struct {
			ProviderID       int64  `json:"providerId"`
			PublicProviderID string `json:"publicProviderId"`
			Name             string `json:"name"`
		} `json:"availableProviders"`
		User struct {
			Email string `json:"emailAddress"`
		} `json:"user"`
	}
	if err := c.doJSON(ctx, http.MethodGet, c.ascBaseURL+"/olympus/v1/session", nil, &payload, nil); err != nil {
		return fmt.Errorf("get App Store Connect session: %w", err)
	}
	session.Provider = Provider{ID: payload.Provider.ProviderID, PublicID: payload.Provider.PublicProviderID, Name: payload.Provider.Name}
	session.Providers = make([]Provider, len(payload.AvailableProviders))
	for i, provider := range payload.AvailableProviders {
		session.Providers[i] = Provider{ID: provider.ProviderID, PublicID: provider.PublicProviderID, Name: provider.Name}
	}
	if payload.User.Email != "" {
		session.Email = payload.User.Email
	}
	return nil
}

type twoFactorStateError struct {
	sessionID string
	scnt      string
}

func (*twoFactorStateError) Error() string { return "two-factor authentication required" }

func (c *Client) performSRPLogin(ctx context.Context, email, password, serviceKey string) error {
	// Apple uses the RFC 5054 2048-bit group, which the standard library does
	// not expose.
	n, ok := new(big.Int).SetString(appleSRPModulusHex, 16)
	if !ok {
		return errors.New("invalid Apple SRP group")
	}
	g := big.NewInt(2)
	aBytes := make([]byte, clientSecretBytes)
	if _, err := rand.Read(aBytes); err != nil {
		return err
	}
	a := new(big.Int).SetBytes(aBytes)
	A := new(big.Int).Exp(g, a, n)
	var init signinInitResponse
	body := map[string]any{"a": base64.StdEncoding.EncodeToString(A.Bytes()), "accountName": email, "protocols": []string{"s2k", "s2k_fo"}}
	if err := c.authJSON(ctx, http.MethodPost, "/signin/init", serviceKey, "", "", body, &init); err != nil {
		return fmt.Errorf("initialize Apple sign-in: %w", err)
	}
	salt, err := base64.StdEncoding.DecodeString(init.Salt)
	if err != nil {
		return err
	}
	prepared, err := preparePassword(password, init.Protocol)
	if err != nil {
		return err
	}
	derived, err := pbkdf2.Key(sha256.New, string(prepared), salt, init.Iteration, derivedPasswordLen)
	if err != nil {
		return err
	}
	serverB, err := base64.StdEncoding.DecodeString(init.ServerPubB)
	if err != nil {
		return err
	}
	m1, m2, err := calculateProof(email, a, A, n, g, serverB, derived, salt)
	if err != nil {
		return err
	}
	hashcash, err := c.hashcash(ctx, serviceKey)
	if err != nil {
		return err
	}
	complete := map[string]any{"accountName": email, "c": init.Challenge, "m1": m1, "m2": m2, "rememberMe": false}
	bodyBytes, _ := json.Marshal(complete)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.authBaseURL+"/signin/complete?isRememberMeEnabled=false", bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	c.setAuthHeaders(req, serviceKey, "", "")
	if hashcash != "" {
		req.Header.Set("X-Apple-HC", hashcash)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(resp.Body)
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusConflict:
		return &twoFactorStateError{sessionID: resp.Header.Get("X-Apple-ID-Session-Id"), scnt: resp.Header.Get("scnt")}
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrInvalidCredentials
	case http.StatusPreconditionFailed:
		return ErrAccountAction
	default:
		return fmt.Errorf("the Apple sign-in failed with status %d: %s", resp.StatusCode, appleErrorMessage(responseBody))
	}
}

func (c *Client) authRequest(ctx context.Context, session *Session, method, path string, body, out any) error {
	return c.authJSON(ctx, method, path, session.ServiceKey, session.AppleIDSessionID, session.SCNT, body, out)
}

func (c *Client) authJSON(ctx context.Context, method, path, serviceKey, sessionID, scnt string, body, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.authBaseURL+path, reader)
	if err != nil {
		return err
	}
	c.setAuthHeaders(req, serviceKey, sessionID, scnt)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("the Apple authentication request failed with status %d: %s", resp.StatusCode, appleErrorMessage(responseBody))
	}
	if out == nil || len(responseBody) == 0 {
		return nil
	}
	return json.Unmarshal(responseBody, out)
}

func (c *Client) setAuthHeaders(req *http.Request, serviceKey, sessionID, scnt string) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-Apple-Widget-Key", serviceKey)
	if sessionID != "" {
		req.Header.Set("X-Apple-ID-Session-Id", sessionID)
	}
	if scnt != "" {
		req.Header.Set("scnt", scnt)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func quoteDESCookies(header http.Header) {
	values := header.Values("Cookie")
	if len(values) == 0 {
		return
	}
	parts := strings.Split(strings.Join(values, "; "), ";")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		name, value, found := strings.Cut(part, "=")
		if found && strings.Contains(name, "DES") && !strings.HasPrefix(value, "\"") {
			part = name + "=\"" + value + "\""
		}
		parts[i] = part
	}
	header.Set("Cookie", strings.Join(parts, "; "))
}

func (c *Client) doJSON(ctx context.Context, method, endpoint string, body, out any, headers http.Header) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("the Apple request failed with status %d: %s", resp.StatusCode, appleErrorMessage(responseBody))
	}
	if out == nil || len(responseBody) == 0 {
		return nil
	}
	return json.Unmarshal(responseBody, out)
}

func appleErrorMessage(body []byte) string {
	var payload struct {
		Errors []struct {
			Detail string `json:"detail"`
			Title  string `json:"title"`
		} `json:"errors"`
		ServiceErrors []struct {
			Message string `json:"message"`
			Title   string `json:"title"`
		} `json:"serviceErrors"`
	}
	if json.Unmarshal(body, &payload) == nil {
		if len(payload.Errors) > 0 {
			if payload.Errors[0].Detail != "" {
				return payload.Errors[0].Detail
			}
			return payload.Errors[0].Title
		}
		if len(payload.ServiceErrors) > 0 {
			if payload.ServiceErrors[0].Message != "" {
				return payload.ServiceErrors[0].Message
			}
			return payload.ServiceErrors[0].Title
		}
	}
	return "request rejected"
}

func (c *Client) hashcash(ctx context.Context, serviceKey string) (string, error) {
	endpoint := c.authBaseURL + "/signin?widgetKey=" + url.QueryEscape(serviceKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get Apple sign-in challenge: status %d", resp.StatusCode)
	}
	bitsText := resp.Header.Get("X-Apple-HC-Bits")
	challenge := resp.Header.Get("X-Apple-HC-Challenge")
	if bitsText == "" || challenge == "" {
		return "", nil
	}
	bits, err := strconv.Atoi(bitsText)
	if err != nil {
		return "", err
	}
	return makeHashcash(bits, challenge, time.Now().UTC()), nil
}

func makeHashcash(bits int, challenge string, now time.Time) string {
	date := now.Format("20060102150405")
	for counter := 0; ; counter++ {
		candidate := fmt.Sprintf("1:%d:%s:%s::%d", bits, date, challenge, counter)
		sum := sha1.Sum([]byte(candidate))
		if leadingZeroBits(sum[:], bits) {
			return candidate
		}
	}
}

func leadingZeroBits(sum []byte, bits int) bool {
	for i := 0; i < bits/8; i++ {
		if sum[i] != 0 {
			return false
		}
	}
	remaining := bits % 8
	return remaining == 0 || sum[bits/8]&(0xff<<(8-remaining)) == 0
}

func preparePassword(password, protocol string) ([]byte, error) {
	digest := sha256.Sum256([]byte(password))
	switch protocol {
	case "s2k":
		return digest[:], nil
	case "s2k_fo":
		return []byte(hex.EncodeToString(digest[:])), nil
	default:
		return nil, fmt.Errorf("unsupported Apple SRP protocol %q", protocol)
	}
}

func calculateProof(username string, a, A, n, g *big.Int, serverB, password, salt []byte) (string, string, error) {
	bHex, saltHex, aHex := hex.EncodeToString(serverB), hex.EncodeToString(salt), numberHex(A)
	xInner, err := shaHex("3a" + hex.EncodeToString(password))
	if err != nil {
		return "", "", err
	}
	xHex, err := shaHex(saltHex + xInner)
	if err != nil {
		return "", "", err
	}
	x, _ := new(big.Int).SetString(xHex, 16)
	k, err := paddedHash(n, numberHex(n), numberHex(g))
	if err != nil {
		return "", "", err
	}
	u, err := paddedHash(n, aHex, bHex)
	if err != nil || u.Sign() == 0 {
		return "", "", errors.New("invalid Apple SRP scrambling parameter")
	}
	B := new(big.Int).SetBytes(serverB)
	base := new(big.Int).Sub(B, new(big.Int).Mod(new(big.Int).Mul(k, new(big.Int).Exp(g, x, n)), n))
	base.Mod(base, n)
	exponent := new(big.Int).Add(a, new(big.Int).Mul(u, x))
	secret := new(big.Int).Exp(base, exponent, n)
	sessionKey, err := shaHex(numberHex(secret))
	if err != nil {
		return "", "", err
	}
	hn, _ := paddedHash(n, numberHex(n))
	hg, _ := paddedHash(n, numberHex(g))
	proofInput := numberHex(new(big.Int).Xor(hn, hg)) + shaStringHex(username) + saltHex + aHex + bHex + sessionKey
	m1, err := shaHex(proofInput)
	if err != nil {
		return "", "", err
	}
	m2, err := shaHex(aHex + m1 + sessionKey)
	if err != nil {
		return "", "", err
	}
	m1Bytes, _ := hex.DecodeString(m1)
	m2Bytes, _ := hex.DecodeString(m2)
	return base64.StdEncoding.EncodeToString(m1Bytes), base64.StdEncoding.EncodeToString(m2Bytes), nil
}

func paddedHash(n *big.Int, values ...string) (*big.Int, error) {
	nLen := 2 * (((len(fmt.Sprintf("%x", n)) * 4) + 7) >> 3)
	var input strings.Builder
	for _, value := range values {
		if len(value) > nLen {
			return nil, errors.New("the Apple SRP value exceeds group width")
		}
		input.WriteString(strings.Repeat("0", nLen-len(value)))
		input.WriteString(strings.ToLower(value))
	}
	digest, err := shaHex(input.String())
	if err != nil {
		return nil, err
	}
	result, _ := new(big.Int).SetString(digest, 16)
	result.Mod(result, n)
	return result, nil
}

func shaHex(value string) (string, error) {
	raw, err := hex.DecodeString(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func shaStringHex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func numberHex(number *big.Int) string {
	value := strings.ToLower(number.Text(16))
	if len(value)%2 == 1 {
		value = "0" + value
	}
	return value
}
