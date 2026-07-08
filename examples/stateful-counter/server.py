from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

DATA_FILE = Path("/data/counter.txt")


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        DATA_FILE.parent.mkdir(parents=True, exist_ok=True)
        try:
            current = int(DATA_FILE.read_text().strip())
        except FileNotFoundError:
            current = 0
        except ValueError:
            current = 0

        current += 1
        DATA_FILE.write_text(f"{current}\n")

        body = f"SSHDock stateful counter OK count={current}\n".encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format, *args):
        print(format % args)


if __name__ == "__main__":
    HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
