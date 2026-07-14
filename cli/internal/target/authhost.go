package target

import (
	"net"
	"slices"
	"strconv"
	"strings"
)

// authHost derives the host binding for a target's OAuth tokens: the
// normalized endpoint set the connection string names. Stored tokens are
// keyed by it (alongside issuer and client ID), so a token obtained for one
// host is never attached to a connection to another - a config that reuses an
// org's issuer/clientID but points the connection elsewhere finds no token
// and falls back to a fresh sign-in against that host (UI-1836).
//
// Normalization goes through the kurrentdb parser (default port, multi-node
// and gossip-seed splitting) so the same real target keys consistently
// regardless of connection-string spelling: hosts lowercased, ports always
// explicit, the set sorted and deduplicated. A multi-node connection keys as
// one set - the client sends the token to whichever node it picks, so the set
// is the trust unit; adding a host changes the key and forces a fresh sign-in.
// The scheme is excluded: with +discover the seed host's DNS/gossip decides
// where connections land, so the seed set is the trust anchor with or without
// discovery.
func authHost(connection string) (string, error) {
	cfg, err := ParseConnection(connection)
	if err != nil {
		return "", err
	}
	var hosts []string
	if cfg.Address != "" {
		hosts = append(hosts, strings.ToLower(cfg.Address))
	}
	for _, ep := range cfg.GossipSeeds {
		// JoinHostPort brackets a host containing colons; the client's parser
		// currently rejects IPv6 literals, so every reachable input formats
		// as plain host:port, but a key this security-sensitive shouldn't
		// depend on that staying true.
		hosts = append(hosts, strings.ToLower(net.JoinHostPort(ep.Host, strconv.Itoa(int(ep.Port)))))
	}
	slices.Sort(hosts)
	return strings.Join(slices.Compact(hosts), ","), nil
}
