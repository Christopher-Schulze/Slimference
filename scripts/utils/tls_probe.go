package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/tlsdial"
)

const (
	tlsProbeDefaultProfile = "chromium_stable"
	tlsProbeReadTimeout    = 2 * time.Second
	tlsProbeMaxRecordLen   = 1 << 16
)

type tlsProbeReport struct {
	Profile               string   `json:"profile"`
	CatalogVersion        string   `json:"catalog_version"`
	CatalogGenerated      string   `json:"catalog_generated"`
	ClientHelloBytes      int      `json:"client_hello_bytes"`
	RecordVersion         string   `json:"record_version"`
	LegacyVersion         string   `json:"legacy_version"`
	CipherSuiteCount      int      `json:"cipher_suite_count"`
	ExtensionCount        int      `json:"extension_count"`
	ALPN                  []string `json:"alpn,omitempty"`
	SNI                   string   `json:"sni,omitempty"`
	SupportedVersions     []string `json:"supported_versions,omitempty"`
	JA3                   string   `json:"ja3"`
	JA3Hash               string   `json:"ja3_hash"`
	GoStdlibJA3Hash       string   `json:"go_stdlib_ja3_hash,omitempty"`
	DiffersFromGoStdlib   bool     `json:"differs_from_go_stdlib"`
	ExternalProofRequired bool     `json:"external_proof_required"`
	Note                  string   `json:"note"`
}

type tlsClientHello struct {
	recordVersion    uint16
	legacyVersion    uint16
	cipherSuites     []uint16
	extensions       []uint16
	supportedGroups  []uint16
	ecPointFormats   []byte
	alpn             []string
	sni              string
	supportedVersion []uint16
}

func runTLSProbe(args []string, stdout, stderr io.Writer) int {
	opts, err := parseTLSProbeArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	profile, err := tlsdial.ResolveProfile(opts.profile)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	report, err := buildTLSProbeReport(profile)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if opts.json {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	writeTLSProbeText(stdout, report)
	return 0
}

type tlsProbeOptions struct {
	profile string
	json    bool
}

func parseTLSProbeArgs(args []string) (tlsProbeOptions, error) {
	opts := tlsProbeOptions{profile: tlsProbeDefaultProfile}
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch {
		case arg == "":
			continue
		case arg == "--json":
			opts.json = true
		case arg == "--profile":
			if i+1 >= len(args) {
				return tlsProbeOptions{}, errors.New("Usage: tls-probe [--profile=<name>] [--json]")
			}
			i++
			opts.profile = args[i]
		case strings.HasPrefix(arg, "--profile="):
			opts.profile = strings.TrimPrefix(arg, "--profile=")
		default:
			return tlsProbeOptions{}, fmt.Errorf("unknown tls-probe arg %q\nUsage: tls-probe [--profile=<name>] [--json]", arg)
		}
	}
	return opts, nil
}

func buildTLSProbeReport(profile tlsdial.Profile) (tlsProbeReport, error) {
	record, err := captureTLSClientHello(profile)
	if err != nil {
		return tlsProbeReport{}, err
	}
	hello, err := parseTLSClientHello(record)
	if err != nil {
		return tlsProbeReport{}, err
	}
	report := tlsProbeReportFromHello(profile, record, hello)
	stdlib, err := tlsdial.ResolveProfile("go_stdlib")
	if err != nil {
		return tlsProbeReport{}, err
	}
	if profile.Name != stdlib.Name {
		stdRecord, err := captureTLSClientHello(stdlib)
		if err != nil {
			return tlsProbeReport{}, err
		}
		stdHello, err := parseTLSClientHello(stdRecord)
		if err != nil {
			return tlsProbeReport{}, err
		}
		report.GoStdlibJA3Hash = ja3Hash(ja3String(stdHello))
		report.DiffersFromGoStdlib = report.JA3Hash != report.GoStdlibJA3Hash
	}
	return report, nil
}

func captureTLSClientHello(profile tlsdial.Profile) ([]byte, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen probe endpoint: %w", err)
	}
	defer ln.Close()

	recordCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()
		if err := conn.SetReadDeadline(time.Now().Add(tlsProbeReadTimeout)); err != nil {
			errCh <- err
			return
		}
		record, err := readTLSRecord(conn)
		if err != nil {
			errCh <- err
			return
		}
		recordCh <- record
	}()

	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), tlsProbeReadTimeout)
	defer cancel()
	conn, dialErr := tlsdial.Dial(ctx, "tcp", host, port, profile)
	if conn != nil {
		_ = conn.Close()
	}
	select {
	case record := <-recordCh:
		return record, nil
	case err := <-errCh:
		if dialErr != nil {
			return nil, fmt.Errorf("capture client hello after dial error %v: %w", dialErr, err)
		}
		return nil, err
	case <-ctx.Done():
		if dialErr != nil {
			return nil, fmt.Errorf("capture client hello timed out after dial error: %w", dialErr)
		}
		return nil, ctx.Err()
	}
}

func readTLSRecord(r io.Reader) ([]byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("read tls record header: %w", err)
	}
	if header[0] != 0x16 {
		return nil, fmt.Errorf("unexpected tls record type 0x%02x", header[0])
	}
	recordLen := int(header[3])<<8 | int(header[4])
	if recordLen <= 0 || recordLen > tlsProbeMaxRecordLen {
		return nil, fmt.Errorf("invalid tls record length %d", recordLen)
	}
	body := make([]byte, recordLen)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("read tls record body: %w", err)
	}
	return append(header, body...), nil
}

func parseTLSClientHello(record []byte) (tlsClientHello, error) {
	if len(record) < 9 {
		return tlsClientHello{}, errors.New("tls record too short")
	}
	if record[0] != 0x16 {
		return tlsClientHello{}, fmt.Errorf("unexpected tls record type 0x%02x", record[0])
	}
	recordLen := int(record[3])<<8 | int(record[4])
	if recordLen != len(record)-5 {
		return tlsClientHello{}, fmt.Errorf("tls record length mismatch got %d want %d", recordLen, len(record)-5)
	}
	handshake := record[5:]
	if len(handshake) < 4 || handshake[0] != 0x01 {
		return tlsClientHello{}, errors.New("tls record is not a client hello")
	}
	helloLen := int(handshake[1])<<16 | int(handshake[2])<<8 | int(handshake[3])
	if helloLen > len(handshake)-4 {
		return tlsClientHello{}, fmt.Errorf("client hello length %d exceeds record body %d", helloLen, len(handshake)-4)
	}
	body := handshake[4 : 4+helloLen]
	p := byteParser{data: body}
	hello := tlsClientHello{recordVersion: readU16(record[1:3])}
	var ok bool
	if hello.legacyVersion, ok = p.u16(); !ok {
		return tlsClientHello{}, errors.New("missing client hello legacy_version")
	}
	if !p.skip(32) {
		return tlsClientHello{}, errors.New("missing client random")
	}
	sessionIDLen, ok := p.u8()
	if !ok || !p.skip(int(sessionIDLen)) {
		return tlsClientHello{}, errors.New("invalid session id block")
	}
	cipherLen, ok := p.u16()
	if !ok || cipherLen%2 != 0 || !p.has(int(cipherLen)) {
		return tlsClientHello{}, errors.New("invalid cipher suite block")
	}
	for i := 0; i < int(cipherLen); i += 2 {
		v, _ := p.u16()
		hello.cipherSuites = append(hello.cipherSuites, v)
	}
	compressionLen, ok := p.u8()
	if !ok || !p.skip(int(compressionLen)) {
		return tlsClientHello{}, errors.New("invalid compression methods block")
	}
	if !p.remaining() {
		return hello, nil
	}
	extLen, ok := p.u16()
	if !ok || !p.has(int(extLen)) {
		return tlsClientHello{}, errors.New("invalid extension block")
	}
	extBytes, _ := p.bytes(int(extLen))
	if err := parseTLSExtensions(extBytes, &hello); err != nil {
		return tlsClientHello{}, err
	}
	return hello, nil
}

func parseTLSExtensions(data []byte, hello *tlsClientHello) error {
	p := byteParser{data: data}
	for p.remaining() {
		extType, ok := p.u16()
		if !ok {
			return errors.New("truncated extension type")
		}
		extLen, ok := p.u16()
		if !ok || !p.has(int(extLen)) {
			return fmt.Errorf("invalid extension length for %d", extType)
		}
		extData, _ := p.bytes(int(extLen))
		hello.extensions = append(hello.extensions, extType)
		switch extType {
		case 0:
			hello.sni = parseServerName(extData)
		case 10:
			hello.supportedGroups = parseU16Vector(extData)
		case 11:
			hello.ecPointFormats = parseU8Vector(extData)
		case 16:
			hello.alpn = parseALPN(extData)
		case 43:
			hello.supportedVersion = parseSupportedVersions(extData)
		}
	}
	return nil
}

type byteParser struct {
	data []byte
	pos  int
}

func (p *byteParser) remaining() bool {
	return p.pos < len(p.data)
}

func (p *byteParser) has(n int) bool {
	return n >= 0 && p.pos+n <= len(p.data)
}

func (p *byteParser) skip(n int) bool {
	if !p.has(n) {
		return false
	}
	p.pos += n
	return true
}

func (p *byteParser) u8() (byte, bool) {
	if !p.has(1) {
		return 0, false
	}
	v := p.data[p.pos]
	p.pos++
	return v, true
}

func (p *byteParser) u16() (uint16, bool) {
	if !p.has(2) {
		return 0, false
	}
	v := readU16(p.data[p.pos : p.pos+2])
	p.pos += 2
	return v, true
}

func (p *byteParser) bytes(n int) ([]byte, bool) {
	if !p.has(n) {
		return nil, false
	}
	v := p.data[p.pos : p.pos+n]
	p.pos += n
	return v, true
}

func parseServerName(data []byte) string {
	p := byteParser{data: data}
	listLen, ok := p.u16()
	if !ok || !p.has(int(listLen)) {
		return ""
	}
	list, _ := p.bytes(int(listLen))
	q := byteParser{data: list}
	for q.remaining() {
		nameType, ok := q.u8()
		if !ok {
			return ""
		}
		nameLen, ok := q.u16()
		if !ok || !q.has(int(nameLen)) {
			return ""
		}
		name, _ := q.bytes(int(nameLen))
		if nameType == 0 {
			return string(name)
		}
	}
	return ""
}

func parseU16Vector(data []byte) []uint16 {
	p := byteParser{data: data}
	listLen, ok := p.u16()
	if !ok || listLen%2 != 0 || !p.has(int(listLen)) {
		return nil
	}
	out := make([]uint16, 0, int(listLen)/2)
	for i := 0; i < int(listLen); i += 2 {
		v, _ := p.u16()
		out = append(out, v)
	}
	return out
}

func parseU8Vector(data []byte) []byte {
	p := byteParser{data: data}
	listLen, ok := p.u8()
	if !ok || !p.has(int(listLen)) {
		return nil
	}
	out, _ := p.bytes(int(listLen))
	return append([]byte(nil), out...)
}

func parseALPN(data []byte) []string {
	p := byteParser{data: data}
	listLen, ok := p.u16()
	if !ok || !p.has(int(listLen)) {
		return nil
	}
	list, _ := p.bytes(int(listLen))
	q := byteParser{data: list}
	var out []string
	for q.remaining() {
		nameLen, ok := q.u8()
		if !ok || !q.has(int(nameLen)) {
			return nil
		}
		name, _ := q.bytes(int(nameLen))
		out = append(out, string(name))
	}
	return out
}

func parseSupportedVersions(data []byte) []uint16 {
	p := byteParser{data: data}
	listLen, ok := p.u8()
	if !ok || listLen%2 != 0 || !p.has(int(listLen)) {
		return nil
	}
	out := make([]uint16, 0, int(listLen)/2)
	for i := 0; i < int(listLen); i += 2 {
		v, _ := p.u16()
		out = append(out, v)
	}
	return out
}

func tlsProbeReportFromHello(profile tlsdial.Profile, record []byte, hello tlsClientHello) tlsProbeReport {
	info := tlsdial.Catalog()
	ja3 := ja3String(hello)
	return tlsProbeReport{
		Profile:               profile.Name,
		CatalogVersion:        info.Version,
		CatalogGenerated:      info.Generated.Format("2006-01-02"),
		ClientHelloBytes:      len(record),
		RecordVersion:         tlsVersionString(hello.recordVersion),
		LegacyVersion:         tlsVersionString(hello.legacyVersion),
		CipherSuiteCount:      len(hello.cipherSuites),
		ExtensionCount:        len(hello.extensions),
		ALPN:                  hello.alpn,
		SNI:                   hello.sni,
		SupportedVersions:     tlsVersionStrings(hello.supportedVersion),
		JA3:                   ja3,
		JA3Hash:               ja3Hash(ja3),
		ExternalProofRequired: true,
		Note:                  "Local ClientHello capture proves the selected profile is wired and removes the Go-stdlib tell; provider-edge JA3/JA4 parity still requires an external reflected probe.",
	}
}

func writeTLSProbeText(w io.Writer, report tlsProbeReport) {
	fmt.Fprintln(w, "=== TLS Probe ===")
	fmt.Fprintf(w, "Profile:              %s\n", report.Profile)
	fmt.Fprintf(w, "Catalog:              %s (%s)\n", report.CatalogVersion, report.CatalogGenerated)
	fmt.Fprintf(w, "ClientHello bytes:    %d\n", report.ClientHelloBytes)
	fmt.Fprintf(w, "Record version:       %s\n", report.RecordVersion)
	fmt.Fprintf(w, "Legacy version:       %s\n", report.LegacyVersion)
	fmt.Fprintf(w, "Cipher suites:        %d\n", report.CipherSuiteCount)
	fmt.Fprintf(w, "Extensions:           %d\n", report.ExtensionCount)
	if len(report.ALPN) > 0 {
		fmt.Fprintf(w, "ALPN:                 %s\n", strings.Join(report.ALPN, ","))
	}
	if len(report.SupportedVersions) > 0 {
		fmt.Fprintf(w, "Supported versions:   %s\n", strings.Join(report.SupportedVersions, ","))
	}
	fmt.Fprintf(w, "JA3:                  %s\n", report.JA3)
	fmt.Fprintf(w, "JA3 hash:             %s\n", report.JA3Hash)
	if report.GoStdlibJA3Hash != "" {
		fmt.Fprintf(w, "Go stdlib JA3 hash:   %s\n", report.GoStdlibJA3Hash)
		fmt.Fprintf(w, "Differs from stdlib:  %t\n", report.DiffersFromGoStdlib)
	}
	fmt.Fprintf(w, "External proof:       required for JA4/provider-edge parity\n")
}

func ja3String(hello tlsClientHello) string {
	parts := []string{
		strconv.Itoa(int(hello.legacyVersion)),
		joinU16(filterGREASEU16(hello.cipherSuites)),
		joinU16(filterGREASEU16(hello.extensions)),
		joinU16(filterGREASEU16(hello.supportedGroups)),
		joinBytes(hello.ecPointFormats),
	}
	return strings.Join(parts, ",")
}

func ja3Hash(ja3 string) string {
	sum := md5.Sum([]byte(ja3))
	return hex.EncodeToString(sum[:])
}

func filterGREASEU16(values []uint16) []uint16 {
	out := make([]uint16, 0, len(values))
	for _, v := range values {
		if !isGREASE(v) {
			out = append(out, v)
		}
	}
	return out
}

func isGREASE(v uint16) bool {
	hi := byte(v >> 8)
	lo := byte(v)
	return hi == lo && lo&0x0f == 0x0a
}

func joinU16(values []uint16) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strconv.Itoa(int(v))
	}
	return strings.Join(parts, "-")
}

func joinBytes(values []byte) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strconv.Itoa(int(v))
	}
	return strings.Join(parts, "-")
}

func tlsVersionStrings(values []uint16) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if isGREASE(v) {
			continue
		}
		out = append(out, tlsVersionString(v))
	}
	return out
}

func tlsVersionString(v uint16) string {
	switch v {
	case 0x0301:
		return "TLS1.0"
	case 0x0302:
		return "TLS1.1"
	case 0x0303:
		return "TLS1.2"
	case 0x0304:
		return "TLS1.3"
	default:
		return fmt.Sprintf("0x%04x", v)
	}
}

func readU16(data []byte) uint16 {
	return uint16(data[0])<<8 | uint16(data[1])
}
