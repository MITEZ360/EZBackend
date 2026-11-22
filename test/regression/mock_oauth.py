"""本地模拟 OAuth2 提供方，用于验证「纯配置接入新渠道」的端到端流程。"""
import json
import urllib.parse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = 9101


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def _json(self, obj, code=200):
        data = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def _redirect(self, url):
        self.send_response(302)
        self.send_header("Location", url)
        self.end_headers()

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        self.rfile.read(length)
        if self.path.startswith("/oidc/token"):
            return self._json({"access_token": "oidc-access-token", "token_type": "Bearer", "expires_in": 3600})
        if self.path.startswith("/gh/login/oauth/access_token"):
            return self._json({"access_token": "gh-access-token", "token_type": "bearer", "scope": "read:user"})
        return self._json({"error": "unknown token endpoint"}, 404)

    def do_GET(self):
        u = urllib.parse.urlparse(self.path)
        q = urllib.parse.parse_qs(u.query)
        if u.path == "/oidc/authorize" or u.path == "/gh/login/oauth/authorize":
            redirect = q.get("redirect_uri", [""])[0]
            state = q.get("state", [""])[0]
            code = "oidc-code" if u.path.startswith("/oidc") else "gh-code"
            sep = "&" if "?" in redirect else "?"
            return self._redirect(f"{redirect}{sep}code={code}&state={urllib.parse.quote(state)}")
        if u.path == "/oidc/userinfo":
            return self._json({
                "sub": "oidc-1001", "preferred_username": "bob", "name": "Bob Chen",
                "email": "bob@example.com", "email_verified": True, "picture": "",
            })
        if u.path == "/gh/user":
            return self._json({
                "id": 8801, "login": "carol", "name": "Carol",
                "email": None, "avatar_url": "",
            })
        if u.path == "/gh/user/emails":
            return self._json([{"email": "carol@example.com", "primary": True, "verified": True}])
        return self._json({"error": "not found"}, 404)


if __name__ == "__main__":
    ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
