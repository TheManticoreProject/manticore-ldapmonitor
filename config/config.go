// Package config holds the parsed options of the tool, grouped so the monitor
// receives one value instead of a dozen positional parameters.
package config

import "github.com/TheManticoreProject/Manticore/windows/credentials"

// Config is the whole configuration of a run.
type Config struct {
	// General
	Debug bool
	// Credentials
	Credentials *credentials.Credentials
	// Network
	Network Network
	// Monitoring
	Monitoring Monitoring
}

// LDAP holds the LDAP transport and bind options.
type LDAP struct {
	UseLdaps    bool
	UseKerberos bool
	// UseSealing requests the GSSAPI confidentiality layer for a Kerberos bind
	// instead of the integrity layer that is negotiated by default.
	UseSealing bool
	LDAPPort   int
	// SPNHostname overrides the hostname used to build the Kerberos ldap SPN when
	// the domain controller is reached by IP. Empty means use the connection host.
	SPNHostname string
}

// Network holds where to connect and as whom.
type Network struct {
	LDAP             LDAP
	DomainController string
	Domain           string
}

// Monitoring holds what to watch and how often.
type Monitoring struct {
	// SearchBase is the single distinguished name to watch. Empty means watch every
	// naming context the domain controller advertises.
	SearchBase string
	// TimeDelay is the number of seconds to wait between two queries.
	TimeDelay int
	// RandomizeDelay picks a random delay between 1 and 5 seconds before each query
	// instead of using TimeDelay.
	RandomizeDelay bool
	// IgnoreUserLogon drops the lastLogon and logonCount changes that every
	// authentication in the domain produces.
	IgnoreUserLogon bool
}
