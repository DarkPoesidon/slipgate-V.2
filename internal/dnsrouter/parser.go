package dnsrouter

import "strings"

// extractDomain extracts the query name from a raw DNS packet.
// Minimal DNS parser — handles standard queries only.
func extractDomain(packet []byte) string {
	if len(packet) < 12 {
		return ""
	}

	// Skip header (12 bytes), read QNAME
	offset := 12
	var labels []string

	for offset < len(packet) {
		length := int(packet[offset])
		if length == 0 {
			break
		}
		// Pointer (compression) — not expected in queries but handle gracefully
		if length&0xC0 == 0xC0 {
			break
		}
		offset++
		if offset+length > len(packet) {
			return ""
		}
		labels = append(labels, string(packet[offset:offset+length]))
		offset += length
	}

	if len(labels) == 0 {
		return ""
	}

	return strings.Join(labels, ".") + "."
}
