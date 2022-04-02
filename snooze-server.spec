# Macros
%undefine _disable_source_fetch
%define venv_cmd virtualenv --always-copy --python=python3.6
%define venv_name snooze
%define venv_install_dir opt/%{venv_name}
%define venv_dir %{buildroot}/%{venv_install_dir}
%define venv_bin %{venv_dir}/bin
%define venv_python %{venv_bin}/python
%define venv_pip %{venv_python} %{venv_bin}/pip install
%define __prelink_undo_cmd %{nil}
%define file_permissions_user snooze
%define file_permissions_group snooze
%define _build_id_links none
%define topdir %(pwd)
%define version %(cat %{topdir}/VERSION)
%define _topdir %{_tmppath}
# Globals
%global __os_install_post %(echo '%{__os_install_post}' | sed -e 's!/usr/lib[^[:space:]]*/brp-python-bytecompile[[:space:]].*$!!g')
# Tags
Name: snooze-server
Version: %{version}
Release: 1
Summary: Snooze server
Group: Application/System
License: AGPL3
AutoReq: No
AutoProv: No
BuildRoot: %{_tmppath}/%{name}-%{version}-build
Source0: https://github.com/snoozeweb/snooze/releases/download/v%{version}/snooze-web-%{version}.tar.gz
# Blocks

%prep
rm -rf %{buildroot}/*
mkdir -p %{buildroot}/%{venv_install_dir}

%clean
rm -rf %{buildroot}

%files
%defattr(-,%{file_permissions_user},%{file_permissions_group},-)
/%{venv_install_dir}
/var/lib/snooze
/var/log/snooze
/etc/snooze
/etc/snooze/server
/usr/lib/systemd/system/snooze-server.service
/opt/snooze/web
%config(noreplace) /etc/snooze/server/core.yaml

%install
%{venv_cmd} %{venv_dir}
%{venv_pip} --trusted-host pypi.org --trusted-host files.pythonhosted.org snooze-server==%{version}
# RECORD files are used by wheels for checksum. They contain path names which
# match the buildroot and must be removed or the package will fail to build.
find %{buildroot} -name "RECORD" -exec rm -rf {} \;
# Strip native modules as they contain buildroot paths intheir debug information
find %{venv_dir}/lib -type f -name "*.so" | xargs -r strip
find %{venv_dir} -name "*.py" -exec sed -i "s+^#\!/.*$+#\!/opt/snooze/bin/python3 -s+g" {} +
find %{venv_dir}/bin -maxdepth 1 -type f -exec sed -i "s+^#\!/.*$+#\!/opt/snooze/bin/python3 -s+g" {} +
mkdir -p %{buildroot}/var/log/snooze
mkdir -p %{buildroot}/etc/snooze/server
mkdir -p %{buildroot}/var/lib/snooze
mkdir -p "%{buildroot}/usr/lib/systemd/system"
cp -R %{topdir}/snooze-server.service %{buildroot}/usr/lib/systemd/system/snooze-server.service
mkdir -p "%{buildroot}/opt/snooze/web"
tar xzf %{SOURCE0} -C %{buildroot}
mkdir -p "%{buildroot}/etc/snooze/server"
cp -R %{topdir}/snooze/defaults/core.yaml %{buildroot}/etc/snooze/server/core.yaml
cp -R %{topdir}/snooze/defaults/logging_file.yaml %{buildroot}/etc/snooze/server/logging.yaml

%build

%description
Snooze server

%pre
id -u %{file_permissions_user} &>/dev/null || useradd %{file_permissions_user}
id -g %{file_permissions_group} &>/dev/null || groupadd %{file_permissions_group}

%post
chown -R %{file_permissions_user}:%{file_permissions_group} /usr/lib/systemd/system/snooze-server.service
chown -R %{file_permissions_user}:%{file_permissions_group} /opt/snooze/web
chown -R %{file_permissions_user}:%{file_permissions_group} /etc/snooze/server/core.yaml

