# Macros
#%undefine _disable_source_fetch
#%define __prelink_undo_cmd %{nil}
#%define _build_id_links none
#%define __arch_install_post %{nil}

# Variables
%define topdir %(pwd)
%define version %(poetry run invoke rpm.version)
%define release %(poetry run invoke rpm.release)
%define snooze_user snooze
%define snooze_group snooze
%define python_version %(poetry run invoke version python)
%define web_version %(poetry run invoke version web)
%define sources %{_topdir}/SOURCES
%define venv %{buildroot}/opt/snooze

# Globals
#%global __os_install_post %(echo '%{__os_install_post}' | sed -e 's!/usr/lib[^[:space:]]*/brp-python-bytecompile[[:space:]].*$!!g')

# Tags
Name: snooze-server
Version: %{version}
Release: %{release}
Summary: Snooze server
Group: Application/System
License: AGPL3
AutoReq: No
AutoProv: No
Source0: snooze-web-%{web_version}.tar.gz
Source1: snooze_server-%{python_version}-py3-none-any.whl
Source2: snooze-server.service
Source3: core.yaml

%description
Snooze server

%prep
rm -rf %{buildroot}/*
mkdir snooze-web
tar -xvf %{sources}/snooze-web-%{web_version}.tar.gz -C snooze-web

%clean
rm -rf %{buildroot}

%install
mkdir -p %{buildroot}/opt/snooze

# Utils directory
mkdir -p %{buildroot}/var/log/snooze
mkdir -p %{buildroot}/var/lib/snooze

# Snooze server
virtualenv --always-copy --python=python3.6 %{venv}
%{venv}/bin/pip install %{sources}/snooze_server-%{python_version}-py3-none-any.whl

# Systemd service
mkdir -p %{buildroot}/usr/lib/systemd/system
cp %{sources}/snooze-server.service %{buildroot}/usr/lib/systemd/system/

# Snooze web
mkdir -p %{venv}/web
cp -r snooze-web/* %{venv}/web

# Default config
mkdir -p %{buildroot}/etc/snooze/server
cp %{sources}/core.yaml %{buildroot}/etc/snooze/server/

find %{venv} -name "*.py" -exec sed -i "s+^#\!/.*$+#\!/opt/snooze/bin/python3 -s+g" {} +
find %{venv}/bin -maxdepth 1 -type f -exec sed -i "s+^#\!/.*$+#\!/opt/snooze/bin/python3 -s+g" {} +

# RECORD files are used by wheels for checksum. They contain path names which
# match the buildroot and must be removed or the package will fail to build.
find %{buildroot} -name "RECORD" -exec rm -rf {} \;
# Strip native modules as they contain buildroot paths intheir debug information
find %{venv}/lib -type f -name "*.so" | xargs -r strip

%files
%defattr(-,%{snooze_user},%{snooze_group},-)
/etc/snooze
/etc/snooze/server
/opt/snooze
/opt/snooze/web
/usr/lib/systemd/system/snooze-server.service
/var/lib/snooze
/var/log/snooze
%config(noreplace) /etc/snooze/server/core.yaml

%build

%pre
id -u %{snooze_user} &>/dev/null || snooze_useradd %{snooze_user}
id -g %{snooze_group} &>/dev/null || snooze_groupadd %{snooze_group}

%post
chown -R %{snooze_user}:%{snooze_group} /usr/lib/systemd/system/snooze-server.service
chown -R %{snooze_user}:%{snooze_group} /opt/snooze/web
chown -R %{snooze_user}:%{snooze_group} /etc/snooze/server/core.yaml
