# Requirements

We use [mise](https://mise.jdx.dev/) to install all dev dependencies
to manage this project.

# Setup

1. Clone snooze repo
```bash
git clone github.com/snoozeweb/snooze
```

2. Install tools (python/nodejs)
```bash
mise trust
mise install
```

3. Build the python code
```bash
task py:build
```

4. Build docker image locally
```bash
echo "LOCAL_REPO=nexus.example.com" > .env.local
task docker:develop
```

5. Prepare local config for kubernetes

Example of `packaging/helm/.helmfile.yaml`
```yaml
---
environments:
  d8:
    kubeContext: dev-cluster
---
releases:
- name: snooze
  namespace: snooze
  chart: .
  values:
  - .values.yaml.gotmpl
```

Example of `packaging/helm/.values.yaml.gotmpl`
```yaml
---
timeZone: Asia/Tokyo

server:
  replicaCount: 3
  image:
    repository: "nexus.example.com/snooze-server"
    tag: "latest"
    pullPolicy: Always
  podMonitor:
    enabled: true
  config:
    defaultAuthBackend: ldap
  ldap:
    # -- Enable LDAP authentication configuration
    enabled: true
    # -- The LDAP host to contact
    host: ad.example.com
    port: 636
    baseDN: ou=users,dc=example,dc=com
    bindDN: CN=my_bind_user,ou=users,dc=example,dc=com
    bindPasswordExistingSecretName: "ldap-bind-password"
    userFilter: '(sAMAccountName=%s)'
    displayNameAttribute: 'cn'
    emailAttribute: 'mail'
    groupDN: ''
    memberAttribute: 'memberOf'

ingress:
  className: nginx
  host: snooze.example.com
  certManager:
    enabled: true
    issuerKind: ClusterIssuer
    issuerName: vault-x1

syslog:
  enabled: true
  image:
    repository: nexus.example.com/snooze-syslog
    tag: develop
    pullPolicy: Always
  debug: true

snmptrap:
  enabled: true
  image:
    repository: nexus.example.com/snooze-snmptrap
    tag: develop
    pullPolicy: Always

googlechat:
  enabled: true
  image:
    repository: nexus.example.com/snooze-googlechat
    tag: develop
    pullPolicy: Always
  botName: "Snooze JNX"
  subscriptionName: "snoozebot-sub"
  existingSaSecretName: "googlechat-sa-secrets"
  httpProxy: "http://proxy.example.com:8080"
  httpsProxy: "http://proxy.example.com:8080"
  noProxy: "192.168.0.0/16,172.16.0.0/12,10.0.0.0/8,example.com"

mongodb:
  storageClassName: my-storage-class
```

5. Create necessary secrets
```bash
# For LDAP login
kubectl create secret generic ldap-bind-password --from-literal=password=...
# For googlechat
kubectl create secret generic googlechat-sa-secrets --from-file=sa_secrets.json
```

6. Deploy chart locally
```bash
task chart:develop
```

# Post-setup

1. Get a root token
```bash
kubectl exec -it deploy/snooze-server -- snooze root-token -s snooze.socket
```

2. Use it to connect to snooze URL

3. Change the admin role to include your specific LDAP group
