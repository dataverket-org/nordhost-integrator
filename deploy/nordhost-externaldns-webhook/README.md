# nordhost-externaldns-webhook

This is not a standalone Helm chart. ExternalDNS is normally installed via
its own upstream chart
(https://kubernetes-sigs.github.io/external-dns/), and the recommended way
to run a webhook provider is as a sidecar container in that same Pod,
listening on localhost - so this just documents how to add this image as
that sidecar.

## 1. Create the credentials Secret

Same DirectAdmin credentials file as the cert-manager webhook - see
`config.json.example` in the repo root.

```sh
kubectl create secret generic nordhost-externaldns-webhook-config \
  --from-file=config.json=./config.json -n external-dns
```

## 2. Add the sidecar to your ExternalDNS values

Merge `values-snippet.yaml` into whatever values file you already use to
install the upstream `external-dns` chart. It uses the chart's native
`provider.webhook.*` support (the chart itself runs the webhook image as a
sidecar and points ExternalDNS's core container at it - no manual
`extraContainers` wiring needed):

- Sets `provider.name: webhook` and `provider.webhook.image.repository` to
  this image.
- Mounts the credentials Secret at `/etc/nordhost/config.json` in the
  webhook sidecar via `provider.webhook.extraVolumeMounts` +
  top-level `extraVolumes`.

```sh
helm upgrade --install external-dns external-dns/external-dns \
  -n external-dns \
  -f values-snippet.yaml \
  --set image.repository=<your registry>/nordhost-externaldns-webhook
```

## Notes

- The webhook provider API (`/`, `/records`, `/adjustendpoints`) is bound
  to `127.0.0.1:8888` only, per the ExternalDNS webhook spec - it's never
  exposed outside the Pod.
- `/healthz` is bound to `0.0.0.0:8080` for kubelet probes.
- `--source` on the core ExternalDNS container is unaffected by this setup
  and should still be set to whatever sources (ingress, service, etc.) you
  want ExternalDNS to watch - `--provider=webhook` only changes where
  records get written.
