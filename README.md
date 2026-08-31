![](./.github/banner.png)

<p align="center">
      A tool to watch, diff, and report the live changes of Active Directory objects (object creations, object deletions, and the before and after values of every attribute) across the naming contexts of a domain over LDAP.
      <br>
      <a href="https://github.com/TheManticoreProject/manticore-ldapmonitor/actions/workflows/release.yaml" title="Build"><img alt="Build and Release" src="https://github.com/TheManticoreProject/manticore-ldapmonitor/actions/workflows/release.yaml/badge.svg"></a>
      <img alt="GitHub release (latest by date)" src="https://img.shields.io/github/v/release/TheManticoreProject/manticore-ldapmonitor">
      <img alt="Go Report Card" src="https://goreportcard.com/badge/github.com/TheManticoreProject/manticore-ldapmonitor">
      <a href="https://twitter.com/intent/follow?screen_name=podalirius_" title="Follow"><img src="https://img.shields.io/twitter/follow/podalirius_?label=Podalirius&style=social"></a>
      <a href="https://www.youtube.com/c/Podalirius_?sub_confirmation=1" title="Subscribe"><img alt="YouTube Channel Subscribers" src="https://img.shields.io/youtube/channel/subscribers/UCF_x5O7CSfr82AfNVTKOv_A?style=social"></a>
      <br>
</p>

With this tool you can quickly see if your attack worked and if it changed the LDAP attributes of the target object.

## Features

- [x] Monitor every naming context the domain controller advertises, or a single subtree with `--search-base`
- [x] Report object creations, object deletions, and the value of every attribute before and after it changed
- [x] Custom delay between two queries with `--time-delay`, or a delay picked at random between 1 and 5 seconds with `--randomize-delay`
- [x] Ignore the `lastLogon` and `logonCount` churn of user logon events with `--ignore-user-logon`
- [x] Append the output to a log file with `--logfile`
- [x] Timestamp attributes (`whenChanged`, `lastLogon`, `pwdLastSet`, `accountExpires`, ...) rendered as dates rather than as raw LDAP or FILETIME values
- [x] Password asked for on the terminal when none is given, so the secret never has to go in the command line
- [x] Colored output, or plain text with `--no-colors`
- [x] Non-text values (`objectSid`, `objectGUID`, `nTSecurityDescriptor`, ...) rendered as hex instead of raw bytes
- [x] Reconnect and carry on when the domain controller drops a long-running session
- [x] LDAP, or LDAPS with `--use-ldaps`, and a signed or encrypted Kerberos session on plain LDAP for a domain controller that enforces LDAP signing
- [x] Authenticate with a password (`-p`), a NT hash (`-H`), an AES key (`--aes-key`), or a Kerberos ticket (`--ticket-ccache`, `--ticket-kirbi`), over Kerberos (`-k`) or NTLM
- [x] Complete values of large multi-valued attributes, past the domain controller's 1500-value cap (MaxValRange), via incremental range retrieval
- [x] Escape sequences in distinguished names, attribute names and values neutralized before they reach the terminal
- [ ] Custom page size for the paged queries
- [ ] Authenticate as the current shell user

## Installation

To get this tool you can either download the latest release from the [GitHub release page](https://github.com/TheManticoreProject/manticore-ldapmonitor/releases) or install it with the following `go` command:

```bash
go install github.com/TheManticoreProject/manticore-ldapmonitor@latest
```

## Usage

```
$ ./manticore-ldapmonitor --help
manticore-ldapmonitor - by Remi GASCOU (Podalirius) @ TheManticoreProject - v1.0.0

Usage: manticore-ldapmonitor --domain <string> --username <string> [--no-pass] [--debug] [--no-colors] [--logfile <string>] --dc-ip <string> [--dc-host <string>] [--ldap-port <tcp port>] [--use-ldaps] [--use-kerberos] [--use-sealing] [--search-base <string>] [--ignore-user-logon] [--time-delay <int>] [--randomize-delay] [--password <string>] [--hashes <string>] [--aes-key <string>] [--ticket-ccache <string>] [--ticket-kirbi <string>]


  Authentication:
    -d, --domain <string>   Active Directory domain to authenticate to.
    -u, --username <string> User to authenticate as.
    --no-pass               Do not ask for a password, bind without one. (default: false)

  Configuration:
    --debug                Debug mode. (default: false)
    --no-colors            Print the output without colors. (default: false)
    -l, --logfile <string> Log file to append the output to. (default: "")

  LDAP Connection Settings:
    -dc, --dc-ip <string>       IP address or hostname of the domain controller to monitor, which is also the KDC (Key Distribution Center) used for Kerberos.
    --dc-host <string>          FQDN of the domain controller, used to build the Kerberos SPN when connecting by IP with -k. (default: "")
    -lp, --ldap-port <tcp port> Port number to connect to LDAP server. (default: 389)
    -L, --use-ldaps             Use LDAPS instead of LDAP. (default: false)
    -k, --use-kerberos          Use Kerberos instead of NTLM. (default: false)
    --use-sealing               Encrypt the LDAP traffic of a Kerberos session (GSSAPI confidentiality) instead of only signing it. Ignored with -L. (default: false)

  Monitoring:
    -S, --search-base <string> Distinguished name to monitor. If omitted, every naming context of the domain controller is monitored. (default: "")
    --ignore-user-logon        Ignore the lastLogon and logonCount changes produced by user logon events. (default: false)

  Query delay:
    -t, --time-delay <int> Delay between two queries, in seconds. (default: 1)
    -r, --randomize-delay  Randomize the delay between two queries, between 1 and 5 seconds. (default: false)

  Secret:
    -p, --password <string>  Password to authenticate with. (default: "")
    -H, --hashes <string>    NT/LM hashes, format is LMhash:NThash. (default: "")
    --aes-key <string>       AES key to use for Kerberos Authentication (128 or 256 bits). (default: "")
    --ticket-ccache <string> Path to a Kerberos credential cache (ccache) holding a TGT for pass-the-ticket (implies -k). (default: "")
    --ticket-kirbi <string>  Path to a .kirbi file holding a TGT for pass-the-ticket (implies -k). (default: "")
```

At most one of the `Secret` options may be given. None is needed on the command
line: the password is asked for on the terminal when it is missing, which keeps it out
of `argv`, where the process list exposes it to every local user, and out of the shell
history. It is read from standard input when that is not a terminal, so a script can
pipe it in. `--no-pass` skips the question and binds without a password, which a
domain controller will only let read the RootDSE.

A domain controller that enforces LDAP signing, which is the default on a current
Windows Server, answers a password or hash bind on plain LDAP with:

```
LDAP Result Code 8 "Strong Auth Required": ... The server requires binds to turn on
integrity checking if SSL\TLS are not already active on the connection
```

Either add `--use-ldaps`, or authenticate with Kerberos (`-k`), which signs the
session on plain LDAP so the domain controller accepts it. Add `--use-sealing` to
encrypt it rather than only signing it. Over LDAPS no security layer is negotiated,
since the TLS channel already protects the connection and Active Directory refuses a
SASL layer on top of it.

### Monitor the whole domain

```
$ ./manticore-ldapmonitor -d MANTICORE.local -u Administrator -p 'Podalirius123!' -dc 192.168.1.101 -L
manticore-ldapmonitor - by Remi GASCOU (Podalirius) @ TheManticoreProject - v1.0.0

[2026-08-31 15h48m15s] [>] Connecting to ldaps://192.168.1.101:636 ...
[2026-08-31 15h48m15s] [+] Authenticated as MANTICORE.local\Administrator.
[2026-08-31 15h48m15s] [>] Monitored search bases (5):
  ├── DC=MANTICORE,DC=local
  ├── CN=Configuration,DC=MANTICORE,DC=local
  ├── CN=Schema,CN=Configuration,DC=MANTICORE,DC=local
  ├── DC=DomainDnsZones,DC=MANTICORE,DC=local
  └── DC=ForestDnsZones,DC=MANTICORE,DC=local
[2026-08-31 15h48m15s] [>] Objects in the initial snapshot: 3689.
[2026-08-31 15h48m15s] [>] Listening for LDAP changes ...
[2026-08-31 15h49m41s] [~] Object updated: DC=dc01,DC=MANTICORE.local,CN=MicrosoftDNS,DC=DomainDnsZones,DC=MANTICORE,DC=local
  ├── Attribute "uSNChanged" changed from '290925' to '290927'
  └── Attribute "whenChanged" changed from '2026-08-31 13:32:22 UTC' to '2026-08-31 13:49:40 UTC'
```

The change above is the domain controller refreshing its own DNS record. The
`dnsRecord` value that moved with it is not reported: it is one of the attributes
the directory rewrites on its own, and it is filtered out so that it does not bury
the changes that somebody actually made.

### Monitor a single subtree

Watching one container instead of the whole domain, with a 2 second delay between
two queries, while an object is created, modified and deleted:

```
$ ./manticore-ldapmonitor -d MANTICORE.local -u Administrator -p 'Podalirius123!' -dc 192.168.1.101 -L \
    -t 2 -S 'CN=Users,DC=MANTICORE,DC=local'
manticore-ldapmonitor - by Remi GASCOU (Podalirius) @ TheManticoreProject - v1.0.0

[2026-08-31 16h35m59s] [>] Connecting to ldaps://192.168.1.101:636 ...
[2026-08-31 16h35m59s] [+] Authenticated as MANTICORE.local\Administrator.
[2026-08-31 16h35m59s] [>] Monitored search bases (1):
  └── CN=Users,DC=MANTICORE,DC=local
[2026-08-31 16h35m59s] [>] Objects in the initial snapshot: 31.
[2026-08-31 16h35m59s] [>] Listening for LDAP changes ...
[2026-08-31 16h36m11s] [+] Object created: CN=TESTOBJ,CN=Users,DC=MANTICORE,DC=local
[2026-08-31 16h36m19s] [~] Object updated: CN=TESTOBJ,CN=Users,DC=MANTICORE,DC=local
  ├── Attribute "otherTelephone" = ['0000000001', '0000000002', '0000000003'] was created
  ├── Attribute "uSNChanged" changed from '290960' to '290961'
  └── Attribute "whenChanged" changed from '2026-08-31 14:36:09 UTC' to '2026-08-31 14:36:17 UTC'
[2026-08-31 16h36m27s] [~] Object updated: CN=TESTOBJ,CN=Users,DC=MANTICORE,DC=local
  ├── Attribute "displayName" = '63616e6172791b5b324a7769706564' was created
  ├── Attribute "uSNChanged" changed from '290961' to '290962'
  └── Attribute "whenChanged" changed from '2026-08-31 14:36:17 UTC' to '2026-08-31 14:36:25 UTC'
[2026-08-31 16h36m36s] [-] Object deleted: CN=TESTOBJ,CN=Users,DC=MANTICORE,DC=local
[2026-08-31 16h38m59s] [>] Interrupt received, stopping after the running query (interrupt again to abort now).
[2026-08-31 16h38m59s] [>] Interrupted, stopping.
[2026-08-31 16h38m59s] Done.
```

Two things in that output are deliberate. The three `otherTelephone` values were
written out of order and are reported in order: the values of an attribute are a set,
the server returns them in an order of its own, and comparing that order rather than
the values would report a change every time it shifted. And `displayName` was set to
a value carrying an ANSI escape sequence, which is rendered hex-encoded rather than
sent to the terminal, where it would have cleared the screen and hidden the change
that had just been reported.

### Quietly, to a log file

```
$ ./manticore-ldapmonitor -d MANTICORE.local -u Administrator -p 'Podalirius123!' -dc 192.168.1.101 -L \
    -S 'OU=Servers,DC=MANTICORE,DC=local' -r --ignore-user-logon -l ldapmonitor.log
```

## Typical use cases

Here are a few use cases where this tool is useful:

- Detect an account lockout in real time, as `badPwdCount` and `lockoutTime` move.
- Check whether a privilege escalation worked, for instance the `member` attribute
  of a group after `ntlmrelayx`'s `--escalate-user`.
- Detect when users log on, to know when to start a network poisoning, by watching
  `lastLogon` and `logonCount`.
- Watch a delegation being written, as `msDS-AllowedToDelegateTo` or
  `msDS-AllowedToActOnBehalfOfOtherIdentity` appear on an account.

## Limitations

A change is found by comparing a full enumeration of the monitored search bases
with the previous one, so a change is reported at the first query that runs after
it lands. The refresh rate is therefore bounded by how long one enumeration takes,
not by `--time-delay`: on a domain with many objects an enumeration can take
several seconds on its own, and `--time-delay` is the pause added on top of it.
Narrowing the scope with `--search-base` is what makes the loop tighter.

Two snapshots are held at a time, the one being compared and the one being built,
so the memory the tool uses grows with the number of objects and attributes in
scope. `--search-base` is again the answer on a large domain.

## Demonstration

<!-- TODO: Add a demonstration -->

## Contributing

Pull requests are welcome. Feel free to open an issue if you want to add other features.

## Credits

  - [p0dalirius](https://github.com/p0dalirius) for the creation of the [LDAPmonitor](https://github.com/p0dalirius/LDAPmonitor) project before transferring it to TheManticoreProject.
