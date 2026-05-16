# Disable debug package and build-id manipulation that breaks static Go binaries.
%global debug_package %{nil}
%global _build_id_links none

# Avoid stripping our already-stripped Go binaries.
%global __os_install_post %{nil}

Name:           snooze
Version:        3.0.0
Release:        1%{?dist}
Summary:        Snooze monitoring server
License:        AGPL-3.0-or-later
URL:            https://snoozeweb.net
Source0:        %{name}-%{version}.tar.gz

BuildRequires:  golang >= 1.25
BuildRequires:  systemd-rpm-macros

%description
Snooze is an event-driven monitoring server that ingests alerts from a wide
variety of sources (syslog, SNMP traps, SMTP, RELP, chat platforms, Pacemaker,
etc.), normalizes them, applies user-defined rules and notifications, and
exposes a web UI plus a REST API. This source RPM ships the Snooze core
server alongside its CLI and the family of protocol bridges as independent
sub-packages so operators can install only what they need.

# ----------------------------------------------------------------------------
# Sub-package: snooze-server (main daemon)
# ----------------------------------------------------------------------------
%package -n snooze-server
Summary:        Snooze main server daemon
Requires(post): systemd
Requires(preun): systemd
Requires(postun): systemd

%description -n snooze-server
The Snooze server daemon: ingests records, evaluates rules, manages
notifications, and serves the REST API and web UI.

# ----------------------------------------------------------------------------
# Sub-package: snooze (CLI)
# ----------------------------------------------------------------------------
%package -n snooze
Summary:        Snooze command-line client (snoozectl)

%description -n snooze
Command-line interface for interacting with a running Snooze server:
manage records, rules, notifications, users, and administrative tasks.

# ----------------------------------------------------------------------------
# Sub-package: snooze-syslog
# ----------------------------------------------------------------------------
%package -n snooze-syslog
Summary:        Snooze syslog bridge
Requires(post): systemd
Requires(preun): systemd
Requires(postun): systemd

%description -n snooze-syslog
Daemon that listens for syslog messages (RFC 3164 / RFC 5424) and forwards
them as Snooze records.

# ----------------------------------------------------------------------------
# Sub-package: snooze-snmptrap
# ----------------------------------------------------------------------------
%package -n snooze-snmptrap
Summary:        Snooze SNMP trap receiver
Requires(post): systemd
Requires(preun): systemd
Requires(postun): systemd

%description -n snooze-snmptrap
Daemon that receives SNMPv1/v2c/v3 traps, resolves MIBs, and forwards them
as Snooze records.

# ----------------------------------------------------------------------------
# Sub-package: snooze-smtp
# ----------------------------------------------------------------------------
%package -n snooze-smtp
Summary:        Snooze SMTP bridge
Requires(post): systemd
Requires(preun): systemd
Requires(postun): systemd

%description -n snooze-smtp
SMTP server that accepts incoming email alerts and forwards them as
Snooze records.

# ----------------------------------------------------------------------------
# Sub-package: snooze-relp
# ----------------------------------------------------------------------------
%package -n snooze-relp
Summary:        Snooze RELP bridge
Requires(post): systemd
Requires(preun): systemd
Requires(postun): systemd

%description -n snooze-relp
Daemon that receives reliable event logging protocol (RELP) messages and
forwards them as Snooze records.

# ----------------------------------------------------------------------------
# Sub-package: snooze-googlechat
# ----------------------------------------------------------------------------
%package -n snooze-googlechat
Summary:        Snooze Google Chat notification bridge
Requires(post): systemd
Requires(preun): systemd
Requires(postun): systemd

%description -n snooze-googlechat
Outbound notification bridge that delivers Snooze notifications to Google
Chat spaces and rooms via webhooks.

# ----------------------------------------------------------------------------
# Sub-package: snooze-mattermost
# ----------------------------------------------------------------------------
%package -n snooze-mattermost
Summary:        Snooze Mattermost notification bridge
Requires(post): systemd
Requires(preun): systemd
Requires(postun): systemd

%description -n snooze-mattermost
Outbound notification bridge that delivers Snooze notifications to
Mattermost channels via incoming webhooks.

# ----------------------------------------------------------------------------
# Sub-package: snooze-teams
# ----------------------------------------------------------------------------
%package -n snooze-teams
Summary:        Snooze Microsoft Teams notification bridge
Requires(post): systemd
Requires(preun): systemd
Requires(postun): systemd

%description -n snooze-teams
Outbound notification bridge that delivers Snooze notifications to
Microsoft Teams channels via incoming webhooks.

# ----------------------------------------------------------------------------
# Sub-package: snooze-pacemaker
# ----------------------------------------------------------------------------
%package -n snooze-pacemaker
Summary:        Snooze Pacemaker cluster alert agent

%description -n snooze-pacemaker
Pacemaker alert agent that forwards cluster resource and node events as
Snooze records. Installed as a one-shot binary invoked by Pacemaker;
no systemd unit.

# ----------------------------------------------------------------------------
# Sub-package: snooze-jira
# ----------------------------------------------------------------------------
%package -n snooze-jira
Summary:        Snooze JIRA notification bridge
Requires(post): systemd
Requires(preun): systemd
Requires(postun): systemd

%description -n snooze-jira
Outbound notification bridge that creates and updates JIRA Cloud tickets
from Snooze alerts. Optionally closes Snooze records when the corresponding
JIRA ticket transitions to a Done status.

# ============================================================================
%prep
%setup -q

# ============================================================================
%build
export GOFLAGS="-trimpath -tags=osusergo,netgo -mod=vendor"
export CGO_ENABLED=0
go build \
    -ldflags "-s -w -X github.com/snoozeweb/snooze/internal/version.Version=%{version}" \
    -o _build/ ./cmd/...

# ============================================================================
%install
rm -rf %{buildroot}

# Binaries
install -d -m 0755 %{buildroot}%{_bindir}
install -p -m 0755 _build/snooze              %{buildroot}%{_bindir}/snooze
install -p -m 0755 _build/snooze-server       %{buildroot}%{_bindir}/snooze-server
install -p -m 0755 _build/snooze-syslog       %{buildroot}%{_bindir}/snooze-syslog
install -p -m 0755 _build/snooze-snmptrap     %{buildroot}%{_bindir}/snooze-snmptrap
install -p -m 0755 _build/snooze-smtp         %{buildroot}%{_bindir}/snooze-smtp
install -p -m 0755 _build/snooze-relp         %{buildroot}%{_bindir}/snooze-relp
install -p -m 0755 _build/snooze-googlechat   %{buildroot}%{_bindir}/snooze-googlechat
install -p -m 0755 _build/snooze-mattermost   %{buildroot}%{_bindir}/snooze-mattermost
install -p -m 0755 _build/snooze-teams        %{buildroot}%{_bindir}/snooze-teams
install -p -m 0755 _build/snooze-pacemaker    %{buildroot}%{_bindir}/snooze-pacemaker
install -p -m 0755 _build/snooze-jira         %{buildroot}%{_bindir}/snooze-jira

# Systemd units (eight daemons; snooze CLI and snooze-pacemaker have none).
install -d -m 0755 %{buildroot}%{_unitdir}
install -p -m 0644 packaging/systemd/snooze-server.service      %{buildroot}%{_unitdir}/snooze-server.service
install -p -m 0644 packaging/systemd/snooze-syslog.service      %{buildroot}%{_unitdir}/snooze-syslog.service
install -p -m 0644 packaging/systemd/snooze-snmptrap.service    %{buildroot}%{_unitdir}/snooze-snmptrap.service
install -p -m 0644 packaging/systemd/snooze-smtp.service        %{buildroot}%{_unitdir}/snooze-smtp.service
install -p -m 0644 packaging/systemd/snooze-relp.service        %{buildroot}%{_unitdir}/snooze-relp.service
install -p -m 0644 packaging/systemd/snooze-googlechat.service  %{buildroot}%{_unitdir}/snooze-googlechat.service
install -p -m 0644 packaging/systemd/snooze-mattermost.service  %{buildroot}%{_unitdir}/snooze-mattermost.service
install -p -m 0644 packaging/systemd/snooze-teams.service       %{buildroot}%{_unitdir}/snooze-teams.service
install -p -m 0644 packaging/systemd/snooze-jira.service        %{buildroot}%{_unitdir}/snooze-jira.service

# Example configuration directory (kept empty; operators drop their own files).
install -d -m 0755 %{buildroot}%{_sysconfdir}/snooze

# ============================================================================
# Files
# ============================================================================
%files -n snooze-server
%{_bindir}/snooze-server
%{_unitdir}/snooze-server.service
%dir %{_sysconfdir}/snooze

%files -n snooze
%{_bindir}/snooze

%files -n snooze-syslog
%{_bindir}/snooze-syslog
%{_unitdir}/snooze-syslog.service

%files -n snooze-snmptrap
%{_bindir}/snooze-snmptrap
%{_unitdir}/snooze-snmptrap.service

%files -n snooze-smtp
%{_bindir}/snooze-smtp
%{_unitdir}/snooze-smtp.service

%files -n snooze-relp
%{_bindir}/snooze-relp
%{_unitdir}/snooze-relp.service

%files -n snooze-googlechat
%{_bindir}/snooze-googlechat
%{_unitdir}/snooze-googlechat.service

%files -n snooze-mattermost
%{_bindir}/snooze-mattermost
%{_unitdir}/snooze-mattermost.service

%files -n snooze-teams
%{_bindir}/snooze-teams
%{_unitdir}/snooze-teams.service

%files -n snooze-pacemaker
%{_bindir}/snooze-pacemaker

%files -n snooze-jira
%{_bindir}/snooze-jira
%{_unitdir}/snooze-jira.service

# ============================================================================
# Scriptlets (daemons only)
# ============================================================================
%post -n snooze-server
%systemd_post snooze-server.service

%preun -n snooze-server
%systemd_preun snooze-server.service

%postun -n snooze-server
%systemd_postun_with_restart snooze-server.service

%post -n snooze-syslog
%systemd_post snooze-syslog.service

%preun -n snooze-syslog
%systemd_preun snooze-syslog.service

%postun -n snooze-syslog
%systemd_postun_with_restart snooze-syslog.service

%post -n snooze-snmptrap
%systemd_post snooze-snmptrap.service

%preun -n snooze-snmptrap
%systemd_preun snooze-snmptrap.service

%postun -n snooze-snmptrap
%systemd_postun_with_restart snooze-snmptrap.service

%post -n snooze-smtp
%systemd_post snooze-smtp.service

%preun -n snooze-smtp
%systemd_preun snooze-smtp.service

%postun -n snooze-smtp
%systemd_postun_with_restart snooze-smtp.service

%post -n snooze-relp
%systemd_post snooze-relp.service

%preun -n snooze-relp
%systemd_preun snooze-relp.service

%postun -n snooze-relp
%systemd_postun_with_restart snooze-relp.service

%post -n snooze-googlechat
%systemd_post snooze-googlechat.service

%preun -n snooze-googlechat
%systemd_preun snooze-googlechat.service

%postun -n snooze-googlechat
%systemd_postun_with_restart snooze-googlechat.service

%post -n snooze-mattermost
%systemd_post snooze-mattermost.service

%preun -n snooze-mattermost
%systemd_preun snooze-mattermost.service

%postun -n snooze-mattermost
%systemd_postun_with_restart snooze-mattermost.service

%post -n snooze-teams
%systemd_post snooze-teams.service

%preun -n snooze-teams
%systemd_preun snooze-teams.service

%postun -n snooze-teams
%systemd_postun_with_restart snooze-teams.service

%post -n snooze-jira
%systemd_post snooze-jira.service

%preun -n snooze-jira
%systemd_preun snooze-jira.service

%postun -n snooze-jira
%systemd_postun_with_restart snooze-jira.service

# ============================================================================
%changelog
* Wed May 14 2026 Florian Dematraz <florian.dematraz@egerie.eu> - 3.0.0-1
- Initial Go rewrite (Phase 7-D): single SRPM emits ten sub-packages
  (snooze-server, snooze, snooze-syslog, snooze-snmptrap, snooze-smtp,
  snooze-relp, snooze-googlechat, snooze-mattermost, snooze-teams,
  snooze-pacemaker). Builds vendored Go module with CGO disabled.
