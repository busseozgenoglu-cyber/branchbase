package pgwire

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func buildMockStartupPacket(user, database string) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0, 0, 0, 0}) // length placeholder

	var proto [4]byte
	binary.BigEndian.PutUint32(proto[:], ProtocolVersion3)
	buf.Write(proto[:])

	buf.WriteString("user")
	buf.WriteByte(0)
	buf.WriteString(user)
	buf.WriteByte(0)

	buf.WriteString("database")
	buf.WriteByte(0)
	buf.WriteString(database)
	buf.WriteByte(0)

	buf.WriteByte(0) // terminating null

	data := buf.Bytes()
	binary.BigEndian.PutUint32(data[0:4], uint32(len(data)))
	return data
}

func TestIsSSLRequest(t *testing.T) {
	t.Parallel()
	var sslPkt [8]byte
	binary.BigEndian.PutUint32(sslPkt[0:4], 8)
	binary.BigEndian.PutUint32(sslPkt[4:8], SSLRequestCode)

	if !IsSSLRequest(sslPkt[:]) {
		t.Errorf("expected IsSSLRequest to return true for valid SSL packet")
	}

	nonSSLPkt := []byte{0, 0, 0, 8, 0, 0, 0, 1}
	if IsSSLRequest(nonSSLPkt) {
		t.Errorf("expected IsSSLRequest to return false for non-SSL packet")
	}

	shortPkt := []byte{0, 0, 8}
	if IsSSLRequest(shortPkt) {
		t.Errorf("expected IsSSLRequest to return false for short packet")
	}
}

func TestParseStartupMessage(t *testing.T) {
	t.Parallel()
	pkt := buildMockStartupPacket("postgres", "myapp_dev")

	msg, err := ParseStartupMessage(pkt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Parameters["user"] != "postgres" {
		t.Errorf("expected user 'postgres', got %q", msg.Parameters["user"])
	}
	if msg.Parameters["database"] != "myapp_dev" {
		t.Errorf("expected database 'myapp_dev', got %q", msg.Parameters["database"])
	}
}

func TestParseStartupMessageShortPacket(t *testing.T) {
	t.Parallel()
	shortPackets := [][]byte{
		{},
		{0, 0, 0},
		{0, 0, 0, 4},
		{0, 0, 0, 7},
	}

	for _, pkt := range shortPackets {
		_, err := ParseStartupMessage(pkt)
		if err != ErrPacketTooShort {
			t.Errorf("expected ErrPacketTooShort for len %d, got %v", len(pkt), err)
		}
	}
}

func TestReadStartupPacketBounds(t *testing.T) {
	t.Parallel()
	// Test packet length < 8
	var tooShort [4]byte
	binary.BigEndian.PutUint32(tooShort[:], 4)
	_, err := ReadStartupPacket(bytes.NewReader(tooShort[:]))
	if err != ErrPacketTooShort {
		t.Errorf("expected ErrPacketTooShort for length 4, got: %v", err)
	}

	// Test packet length > 10240
	var tooLarge [4]byte
	binary.BigEndian.PutUint32(tooLarge[:], 20000)
	_, err = ReadStartupPacket(bytes.NewReader(tooLarge[:]))
	if err != ErrPacketTooLarge {
		t.Errorf("expected ErrPacketTooLarge for length 20000, got: %v", err)
	}

	// Valid small packet (length 8: header + protocol version)
	valid := make([]byte, 8)
	binary.BigEndian.PutUint32(valid[0:4], 8)
	binary.BigEndian.PutUint32(valid[4:8], ProtocolVersion3)
	got, err := ReadStartupPacket(bytes.NewReader(valid))
	if err != nil {
		t.Errorf("expected success for valid 8-byte packet, got: %v", err)
	}
	if !bytes.Equal(got, valid) {
		t.Errorf("expected packet %v, got %v", valid, got)
	}
}

func TestBuildErrorResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		severity string
		code     string
		message  string
	}{
		{
			name:     "backend unavailable",
			severity: "FATAL",
			code:     "08001",
			message:  "BranchBase: unable to connect to backend PostgreSQL",
		},
		{
			name:     "generic error",
			severity: "ERROR",
			code:     "XX000",
			message:  "unexpected proxy error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildErrorResponse(tt.severity, tt.code, tt.message)
			if len(got) < 6 {
				t.Fatalf("error response too short: %d bytes", len(got))
			}
			if got[0] != 'E' {
				t.Fatalf("message type = %q, want 'E'", got[0])
			}
			if length := binary.BigEndian.Uint32(got[1:5]); int(length) != len(got)-1 {
				t.Fatalf("message length = %d, want %d", length, len(got)-1)
			}

			wantPayload := []byte{'S'}
			wantPayload = append(wantPayload, tt.severity...)
			wantPayload = append(wantPayload, 0, 'C')
			wantPayload = append(wantPayload, tt.code...)
			wantPayload = append(wantPayload, 0, 'M')
			wantPayload = append(wantPayload, tt.message...)
			wantPayload = append(wantPayload, 0, 0)
			if !bytes.Equal(got[5:], wantPayload) {
				t.Errorf("payload = %v, want %v", got[5:], wantPayload)
			}
		})
	}
}

func TestRewriteDatabase(t *testing.T) {
	t.Parallel()
	pkt := buildMockStartupPacket("postgres", "myapp_dev")

	rewritten, err := RewriteDatabase(pkt, "myapp_dev_feature_billing")
	if err != nil {
		t.Fatalf("unexpected error rewriting database: %v", err)
	}

	msg, err := ParseStartupMessage(rewritten)
	if err != nil {
		t.Fatalf("unexpected error parsing rewritten packet: %v", err)
	}

	if msg.Parameters["database"] != "myapp_dev_feature_billing" {
		t.Errorf("expected database 'myapp_dev_feature_billing', got %q", msg.Parameters["database"])
	}
	if msg.Parameters["user"] != "postgres" {
		t.Errorf("expected user 'postgres' to remain unchanged, got %q", msg.Parameters["user"])
	}

	// Verify packet length consistency
	expectedLen := binary.BigEndian.Uint32(rewritten[0:4])
	if int(expectedLen) != len(rewritten) {
		t.Errorf("header length %d does not match actual length %d", expectedLen, len(rewritten))
	}
}

func FuzzParseStartupMessage(f *testing.F) {
	// Seed valid packets
	f.Add(buildMockStartupPacket("postgres", "myapp_dev"))
	f.Add(buildMockStartupPacket("user_long_name_test", "database_long_production_replica"))
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0, 8, 0, 3, 0, 0})
	f.Add([]byte{0, 0, 0, 10, 0, 3, 0, 0, 0, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		msg, err := ParseStartupMessage(data)
		if err != nil {
			return
		}
		if msg.Parameters == nil {
			t.Fatal("expected non-nil Parameters map on success")
		}
		// Attempt rewrite on any parsed packet; it should never panic
		_, _ = RewriteDatabase(data, "fuzz_database")
	})
}
