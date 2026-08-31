package main

import (
	"fmt"
	"net"
	"os"

	"github.com/TheManticoreProject/Manticore/logger"
	"github.com/TheManticoreProject/Manticore/windows/credentials"
	"github.com/TheManticoreProject/goopts/parser"

	"github.com/TheManticoreProject/manticore-ldapmonitor/cli"
	"github.com/TheManticoreProject/manticore-ldapmonitor/config"
	"github.com/TheManticoreProject/manticore-ldapmonitor/monitor"
)

// VERSION is the version of the tool, shown in the banner.
const VERSION = "1.0.0"

var (
	// Configuration
	debug    bool
	noColors bool
	logFile  string

	// Monitoring
	searchBase      string
	timeDelay       int
	randomizeDelay  bool
	ignoreUserLogon bool

	// LDAP Connection Settings
	domainController string
	dcHost           string
	ldapPort         int
	useLdaps         bool
	useKerberos      bool
	useSealing       bool

	// Authentication
	authDomain   string
	authUsername string
	authPassword string
	authHashes   string
	authAesKey   string
	authNoPass   bool
	ticketCCache string
	ticketKirbi  string
)

func parseArgs() {
	ap := parser.ArgumentsParser{
		Banner: fmt.Sprintf("manticore-ldapmonitor - by Remi GASCOU (Podalirius) @ TheManticoreProject - v%s", VERSION),
	}
	ap.SetOptShowBannerOnHelp(true)
	ap.SetOptShowBannerOnRun(true)

	if group, err := ap.NewArgumentGroup("Configuration"); err != nil {
		logger.Warn(fmt.Sprintf("Error creating ArgumentGroup: %s", err))
	} else {
		group.NewBoolArgument(&debug, "", "--debug", false, "Debug mode.")
		group.NewBoolArgument(&noColors, "", "--no-colors", false, "Print the output without colors.")
		group.NewStringArgument(&logFile, "-l", "--logfile", "", false, "Log file to append the output to.")
	}

	if group, err := ap.NewArgumentGroup("Monitoring"); err != nil {
		logger.Warn(fmt.Sprintf("Error creating ArgumentGroup: %s", err))
	} else {
		group.NewStringArgument(&searchBase, "-S", "--search-base", "", false, "Distinguished name to monitor. If omitted, every naming context of the domain controller is monitored.")
		group.NewBoolArgument(&ignoreUserLogon, "", "--ignore-user-logon", false, "Ignore the lastLogon and logonCount changes produced by user logon events.")
	}

	// A fixed delay and a randomized one are two answers to the same question, and
	// the randomized one wins when both are set. Making them exclusive says so up
	// front, instead of silently querying every 1 to 5 seconds for a caller who
	// asked for one query a minute.
	if group, err := ap.NewNotRequiredMutuallyExclusiveArgumentGroup("Query delay"); err != nil {
		logger.Warn(fmt.Sprintf("Error creating ArgumentGroup: %s", err))
	} else {
		group.NewIntArgument(&timeDelay, "-t", "--time-delay", 1, false, "Delay between two queries, in seconds.")
		group.NewBoolArgument(&randomizeDelay, "-r", "--randomize-delay", false, "Randomize the delay between two queries, between 1 and 5 seconds.")
	}

	if group, err := ap.NewArgumentGroup("LDAP Connection Settings"); err != nil {
		logger.Warn(fmt.Sprintf("Error creating ArgumentGroup: %s", err))
	} else {
		group.NewStringArgument(&domainController, "-dc", "--dc-ip", "", true, "IP address or hostname of the domain controller to monitor, which is also the KDC (Key Distribution Center) used for Kerberos.")
		group.NewStringArgument(&dcHost, "", "--dc-host", "", false, "FQDN of the domain controller, used to build the Kerberos SPN when connecting by IP with -k.")
		group.NewTcpPortArgument(&ldapPort, "-lp", "--ldap-port", 389, false, "Port number to connect to LDAP server.")
		group.NewBoolArgument(&useLdaps, "-L", "--use-ldaps", false, "Use LDAPS instead of LDAP.")
		group.NewBoolArgument(&useKerberos, "-k", "--use-kerberos", false, "Use Kerberos instead of NTLM.")
		group.NewBoolArgument(&useSealing, "", "--use-sealing", false, "Encrypt the LDAP traffic of a Kerberos session (GSSAPI confidentiality) instead of only signing it. Ignored with -L.")
	}

	if group, err := ap.NewArgumentGroup("Authentication"); err != nil {
		logger.Warn(fmt.Sprintf("Error creating ArgumentGroup: %s", err))
	} else {
		group.NewStringArgument(&authDomain, "-d", "--domain", "", true, "Active Directory domain to authenticate to.")
		group.NewStringArgument(&authUsername, "-u", "--username", "", true, "User to authenticate as.")
		// --no-pass selects "no password", it does not supply a secret, so it does
		// not belong in the mutually exclusive Secret group: its job is to suppress
		// the prompt for a run that is meant to bind without one.
		group.NewBoolArgument(&authNoPass, "", "--no-pass", false, "Do not ask for a password, bind without one.")
	}

	// At most one secret, and none is allowed: a run without one is asked for the
	// password on the terminal rather than being refused, so that the secret does not
	// have to be written into argv, where the process list exposes it.
	if group, err := ap.NewNotRequiredMutuallyExclusiveArgumentGroup("Secret"); err != nil {
		logger.Warn(fmt.Sprintf("Error creating ArgumentGroup: %s", err))
	} else {
		group.NewStringArgument(&authPassword, "-p", "--password", "", false, "Password to authenticate with.")
		group.NewStringArgument(&authHashes, "-H", "--hashes", "", false, "NT/LM hashes, format is LMhash:NThash.")
		group.NewStringArgument(&authAesKey, "", "--aes-key", "", false, "AES key to use for Kerberos Authentication (128 or 256 bits).")
		group.NewStringArgument(&ticketCCache, "", "--ticket-ccache", "", false, "Path to a Kerberos credential cache (ccache) holding a TGT for pass-the-ticket (implies -k).")
		group.NewStringArgument(&ticketKirbi, "", "--ticket-kirbi", "", false, "Path to a .kirbi file holding a TGT for pass-the-ticket (implies -k).")
	}

	ap.Parse()

	// LDAPS listens on 636, so selecting it moves the port unless the caller named
	// one explicitly.
	if !ap.ArgumentIsPresent("--ldap-port") {
		if useLdaps {
			ldapPort = 636
		} else {
			ldapPort = 389
		}
	}
}

func main() {
	parseArgs()

	// The logger emits every level by default, so --debug has to raise the floor
	// itself: without this, logger.Debug output appears in runs that did not ask for
	// it. LevelPrint sits above all others, so results are never filtered either way.
	if debug {
		logger.SetLevel(logger.LevelDebug)
	} else {
		logger.SetLevel(logger.LevelInfo)
	}

	// A change is timestamped to the second: the tool cannot resolve when a change
	// landed any finer than the delay between two queries.
	logger.SetTimePrecision(logger.Seconds)

	if noColors {
		logger.SetNoColors(true)
	}

	if logFile != "" {
		if err := logger.LogToFile(logFile); err != nil {
			logger.Warn(fmt.Sprintf("Error opening log file: %s", err))
			os.Exit(1)
		}
		defer logger.CloseLogFile()
	}

	// No secret on the command line means asking for one, unless the caller said not
	// to. A ticket or an AES key is itself the secret, so no prompt happens then.
	if err := cli.ResolvePassword(authDomain, authUsername, &authPassword, authNoPass, authHashes, authAesKey, ticketCCache, ticketKirbi); err != nil {
		logger.Warn(err.Error())
		os.Exit(1)
	}
	if authNoPass && authPassword == "" && authHashes == "" && authAesKey == "" && ticketCCache == "" && ticketKirbi == "" {
		logger.Warn("No secret was provided and --no-pass is set: the bind will be unauthenticated, which a domain controller will not let read the directory.")
	}

	creds, err := credentials.NewCredentials(authDomain, authUsername, authPassword, authHashes)
	if err != nil {
		logger.Warn(fmt.Sprintf("Error creating credentials struct: %s", err))
		os.Exit(1)
	}

	// An AES key is only usable over Kerberos, so requesting one implies -k rather
	// than being a separate choice the caller has to remember to make.
	if len(authAesKey) > 0 {
		if err := creds.SetAESKey(authAesKey); err != nil {
			logger.Warn(fmt.Sprintf("Error setting AES key: %s", err))
			os.Exit(1)
		}
		if !useKerberos {
			logger.Debug("An AES key was supplied, enabling Kerberos authentication")
			useKerberos = true
		}
	}

	// A Kerberos ticket (ccache or .kirbi) is itself the credential for
	// pass-the-ticket and only works over Kerberos, so supplying one implies -k,
	// just like an AES key.
	if len(ticketCCache) > 0 {
		if err := creds.SetCCache(ticketCCache); err != nil {
			logger.Warn(fmt.Sprintf("Error setting ccache: %s", err))
			os.Exit(1)
		}
	}
	if len(ticketKirbi) > 0 {
		if err := creds.SetKirbi(ticketKirbi); err != nil {
			logger.Warn(fmt.Sprintf("Error setting kirbi: %s", err))
			os.Exit(1)
		}
	}
	if len(ticketCCache) > 0 || len(ticketKirbi) > 0 {
		if !useKerberos {
			logger.Debug("A Kerberos ticket was supplied, enabling Kerberos authentication")
			useKerberos = true
		}
	}

	// Active Directory registers the ldap service principal name under the domain
	// controller's FQDN, so a Kerberos bind that builds the SPN from an IP address is
	// rejected with KDC_ERR_S_PRINCIPAL_UNKNOWN. Saying so up front beats leaving the
	// caller to decode that error.
	if useKerberos && dcHost == "" && net.ParseIP(domainController) != nil {
		logger.Warn(fmt.Sprintf("Kerberos was requested against the IP address %s: pass --dc-host with the FQDN of the domain controller, or the KDC will reject the service principal name.", domainController))
	}

	// A delay of zero would query the domain controller in a tight loop, which is
	// both useless (an enumeration already takes longer than that) and loud.
	if timeDelay < 1 {
		logger.Warn(fmt.Sprintf("A delay of %d seconds is not usable, falling back to 1 second.", timeDelay))
		timeDelay = 1
	}

	cfg := config.Config{
		Debug:       debug,
		Credentials: creds,
		Network: config.Network{
			LDAP: config.LDAP{
				UseLdaps:    useLdaps,
				UseKerberos: useKerberos,
				UseSealing:  useSealing,
				LDAPPort:    ldapPort,
				SPNHostname: dcHost,
			},
			DomainController: domainController,
			Domain:           authDomain,
		},
		Monitoring: config.Monitoring{
			SearchBase:      searchBase,
			TimeDelay:       timeDelay,
			RandomizeDelay:  randomizeDelay,
			IgnoreUserLogon: ignoreUserLogon,
		},
	}

	if err := monitor.Run(cfg); err != nil {
		logger.Warn(fmt.Sprintf("Error monitoring LDAP: %s", err))
		os.Exit(1)
	}

	logger.Print("Done.")
}
