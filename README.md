# nordhost-integrator

DNS automation for zones hosted at [Nordhost](https://nordhost.no), through the DirectAdmin API: a cert-manager
DNS-01 webhook solver and an external-dns webhook provider, from one Go module and one credentials file. It works
against any DirectAdmin host, not only Nordhost's.

| Binary | Image | Runs as |
|---|---|---|
| `cmd/main` | `git.dataverket.org/dataverket/tools/nordhost-integrator` | The cert-manager webhook solver, group `nordhost-integrator`, solver `nordhost-solver`; a Deployment and an `APIService` from `deploy/nordhost-webhook` |
| `cmd/externaldns-webhook` | `git.dataverket.org/dataverket/tools/nordhost-integrator-externaldns` | The external-dns webhook provider, a sidecar in the external-dns pod on `127.0.0.1:8888`; `deploy/nordhost-externaldns-webhook` |

Both read the same `config.json`, once, at start. Every push to `main` publishes `:latest` and `:sha-<short>`; a
`vX.Y.Z` tag publishes `:X.Y.Z`.

## 1. A login key per zone

The integrator calls one DirectAdmin command, `CMD_DNS_CONTROL`, as the DirectAdmin user that owns the zone, with a
**login key** as the password. A login key is a second password for a DirectAdmin user, limited to named commands,
addresses and a lifetime, so the user's real password never leaves the person who holds it. One key per zone, even
when one user owns several zones, so that a key can be rotated or revoked for one zone alone.

In DirectAdmin, logged in as the user that owns the zone: **User Level, Login Keys, Create**.

| Field | Value | Why |
|---|---|---|
| Key Name | `<cluster>-<zone>`, e.g. `prod-example-com` | Tracking only; the login name stays the DirectAdmin username |
| Key Value | the Random button | Long and random; copy it once into `config.json` |
| Expires On | never, or a date with a rotation in the calendar | An expired key stops certificate renewal and DNS updates silently: cert-manager retries forever, external-dns logs 401 |
| Clear Key | off | Only for one-time keys |
| Allow HTM | off | The key is for the API, never for a browser |
| Commands | `CMD_DNS_CONTROL` only | The one command the integrator sends; `CMD_API_DNS_CONTROL` is the same command on the API path and is accepted too |
| Allowed IPs | the cluster's egress addresses, one per line, CIDR allowed | The key is useless from anywhere else |
| Number of Uses | 0, unlimited | A long-running integration |
| Current Password | the user's password | DirectAdmin asks for it to create a key |

Test the key before writing it anywhere, from one of the allowed addresses. The answer is the zone's records:

```sh
curl -sS -u '<username>:<key>' -H 'Content-Type: application/json' \
  -X POST 'https://<host>:2222/CMD_DNS_CONTROL?json=yes' -d '{"domain":"<zone>","json":"yes"}' | jq '.records | length'
```

## 2. config.json

One entry per zone: the zone, the DirectAdmin host, the user and the key. `config.json.example` is the template.

```json
{
  "domains": [
    { "domain": "example.com", "host": "https://web25.nordkapp.net:2222", "username": "examp123", "password": "<login key>" }
  ]
}
```

- `domain` must equal the zone exactly as DirectAdmin names it. cert-manager hands the solver the zone it resolved
  from the record's SOA, and the solver matches it exactly; the external-dns provider advertises every `domain`
  listed as its domain filter, so a name under a zone that is not listed is never touched.
- The file holds credentials. It goes into a Secret and nowhere else; encrypt it at rest in git the way your
  cluster encrypts Secrets, or create the Secret out of band.

```sh
kubectl -n cert-manager create secret generic nordhost-config --from-file=config.json=./config.json
```

## 3. The cert-manager solver

```sh
helm upgrade --install nordhost-webhook deploy/nordhost-webhook -n cert-manager \
  --set existingSecretName=nordhost-config \
  --set image.tag=<version>
```

The chart installs the webhook Deployment, its serving certificate (a self-signed CA through cert-manager), the
`APIService` `v1alpha1.nordhost-integrator` and the RBAC that lets cert-manager's service account create
challenges in that group. Point an issuer at it:

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: ops@example.com
    privateKeySecretRef:
      name: letsencrypt-prod-account-key
    solvers:
      - dns01:
          webhook:
            groupName: nordhost-integrator
            solverName: nordhost-solver
```

The solver takes no `config` block: everything it needs is in `config.json`. Wildcards work; a `*.s3.example.com`
certificate writes `_acme-challenge.s3` in `example.com`.

## 4. The external-dns provider

external-dns is installed from its upstream chart; this image is its webhook provider sidecar. Merge
`deploy/nordhost-externaldns-webhook/values-snippet.yaml` into your values: it sets `provider.name: webhook`, the
image, and mounts the credentials Secret at `/etc/nordhost/config.json`. The Secret may be the same one as the
solver's when both run in one namespace. Keep your `sources` as they are; the provider only changes where records
are written.

```sh
kubectl -n external-dns create secret generic nordhost-config --from-file=config.json=./config.json
helm upgrade --install external-dns external-dns/external-dns -n external-dns \
  -f values.yaml -f deploy/nordhost-externaldns-webhook/values-snippet.yaml
```

external-dns keeps its owner `TXT` beside each record it manages, `a-<name>` for an `A` record, so two clusters with
different `--txt-owner-id` can share a zone and never touch each other's records. That is external-dns's registry,
not this provider's; the provider writes what external-dns asks.

## 5. Verify

- A `Certificate` for a name in the zone reaches `Ready`, and `kubectl describe challenge` shows the solver
  presenting and cleaning up; the challenge `TXT` is gone afterwards.
- A Service or route with a hostname in the zone gets its `A` and owner `TXT` at the zone's authoritative servers
  within external-dns's interval.
- Both pods log a `401` from DirectAdmin when a key is wrong or expired; nothing is created and nothing is deleted.

## 6. Rotate a key

1. Create the new key as in step 1, with a new name.
2. Replace the `password` in `config.json`, recreate the Secret, and restart both pods; they read the file only at
   start:

```sh
kubectl -n cert-manager create secret generic nordhost-config --from-file=config.json=./config.json \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n cert-manager rollout restart deployment nordhost-webhook
kubectl -n external-dns rollout restart deployment external-dns
```

3. Delete the old key in DirectAdmin. Adding a zone is the same procedure with a new entry in `domains`.

## Development

`go build ./...` builds both binaries. `make test` runs the suite against a Kubernetes API server from envtest and
against the **real DirectAdmin account** in the `config.json` that `TEST_CONFIG` names: it creates and deletes
records in that zone, so point it at a zone made for testing, never a production one. The DirectAdmin API facts the
code depends on are in `da/da.go`: records are deleted with `action=select` and the record's own
`name=...&value=...` string, never with `delete`; `json=yes` goes on both the query string and the body.
