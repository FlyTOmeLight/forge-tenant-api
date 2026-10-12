"""Connect the existing local GitLab OIDC client to the Forge login page.

Uses existing Kubernetes credentials in memory. Does not create users or grants.
Requires kubectl, Helm, Ruby (standard YAML parser), and the installed GitLab chart.
"""
import argparse
import base64
import json
import subprocess
import urllib.parse
import urllib.request

p = argparse.ArgumentParser(description=__doc__)
p.add_argument("--namespace", default="forge-tenant-split")
p.add_argument("--gitlab-url", default="http://127.0.0.1:18080")
p.add_argument("--casdoor-url", default="http://127.0.0.1:18500")
p.add_argument("--gitlab-chart", required=True)
a = p.parse_args()
kube = ["kubectl", "--context", "orbstack"]

def get(kind, name, namespace):
    return json.loads(subprocess.check_output(kube + ["-n", namespace, "get", kind, name, "-o", "json"]))

def patch_secret(name, namespace, values):
    subprocess.run(kube + ["-n", namespace, "patch", "secret", name, "--type=merge", "--patch-file=/dev/stdin"], input=json.dumps({"stringData": values}).encode(), check=True, stdout=subprocess.DEVNULL)

env = {k: base64.b64decode(v).decode() for k, v in get("secret", "forge-tenant-env", a.namespace)["data"].items()}
provider_yaml = base64.b64decode(get("secret", "lab-credentials", "tenant-lab")["data"]["provider"])
provider = json.loads(subprocess.check_output(["ruby", "-ryaml", "-rjson", "-e", "print JSON.generate(YAML.safe_load(STDIN.read))"], input=provider_yaml))
client = provider["args"]["client_options"]
client_id = client["identifier"]
callback = a.gitlab_url.rstrip("/") + "/users/auth/openid_connect/callback"
headers = {"Content-Type": "application/json", "Authorization": "Basic " + base64.b64encode((env["CASDOOR_CLIENT_ID"] + ":" + env["CASDOOR_SECRET"]).encode()).decode()}
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

def cas(action, query, body=None):
    req = urllib.request.Request(a.casdoor_url + "/api/" + action + "?" + urllib.parse.urlencode(query), data=json.dumps(body).encode() if body is not None else None, headers=headers)
    with opener.open(req, timeout=30) as response:
        result = json.load(response)
    if result.get("status") != "ok":
        raise RuntimeError("Casdoor rejected " + action)
    return result.get("data")

app = cas("get-application", {"id": "admin/lab-gitlab"})
if app["clientId"] != client_id or not app["enableSigninSession"]:
    raise RuntimeError("GitLab identity application does not match the existing provider")
app["redirectUris"] = [callback]
cas("update-application", {"id": "admin/lab-gitlab"}, app)
issuer = provider["args"]["issuer"].rstrip("/")
provider["args"]["discovery"] = False
client.update({
    "authorization_endpoint": env["PUBLIC_URL"] + "/login/oauth/authorize",
    "token_endpoint": issuer + "/api/login/oauth/access_token",
    "userinfo_endpoint": issuer + "/api/userinfo",
    "jwks_uri": issuer + "/.well-known/jwks",
    "redirect_uri": callback,
})
# JSON is valid YAML; keep the existing client ID and secret, never print them.
patch_secret("lab-credentials", "tenant-lab", {"provider": json.dumps(provider)})
patch_secret("forge-tenant-env", a.namespace, {"GITLAB_OIDC_CLIENT_ID": client_id, "GITLAB_PUBLIC_URL": a.gitlab_url.rstrip("/")})
# The separated coding API generates links using the same public GitLab origin.
coding = get("deployment", "forge-coding-api", a.namespace)
for source in coding["spec"]["template"]["spec"]["containers"][0].get("envFrom", []):
    if "secretRef" in source:
        patch_secret(source["secretRef"]["name"], a.namespace, {"GITLAB_PUBLIC_URL": a.gitlab_url.rstrip("/")})
subprocess.run(kube + ["-n", a.namespace, "rollout", "restart", "deployment/forge-tenant-api", "deployment/forge-coding-api"], check=True)
subprocess.run(["helm", "upgrade", "gitlab", a.gitlab_chart, "--kube-context", "orbstack", "--namespace", "tenant-lab", "--reuse-values", "--set", "global.appConfig.omniauth.autoSignInWithProvider=openid_connect", "--wait", "--timeout", "300s"], check=True)
print("GitLab now uses the Forge login page and the configured public callback.")
