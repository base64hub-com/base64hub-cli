#!/usr/bin/env python3
"""Regenerate testdata/cli.json by running a built b64 binary.

The Go build is the reference: the npm and PyPI CLIs must produce the same
stdout, stderr and exit code for every case. Usage:
    go build -ldflags "-X main.version=1.0.0" -o /tmp/b64 ./cmd/b64 && python3 -I scripts/gen-cli-cases.py /tmp/b64
"""
import json, os, subprocess, sys

PNG = bytes.fromhex("89504e470d0a1a0a0000000d49484452")
CASES = [
    (["Hello, hub!"], b""),
    (["-d", "SGVsbG8sIGh1YiE="], b""),
    (["--url"], bytes.fromhex("fbffbf")),
    (["--url", "--no-pad", "Привет"], b""),
    ([], b""),
    ([], b"foobar"),
    (["-w", "4", "foobar"], b""),
    (["--wrap=4", "foobar"], b""),
    (["foobar", "-w", "4"], b""),
    (["--wrap", "0", "foobar"], b""),
    (["-n", "f"], b""),
    (["--data-uri", "Hello"], b""),
    (["--data-uri", "-m", "text/csv", "a,b"], b""),
    (["--data-uri"], PNG),
    (["--data-uri"], b"\x00\x01\x02"),
    (["--data-uri", "<svg xmlns='http://www.w3.org/2000/svg'/>"], b""),
    (["--validate", "SGVsbG8sIGh1YiE="], b""),
    (["--validate", "--", "-_-_"], b""),
    (["--validate", "a+b_"], b""),
    (["--validate", "Zg"], b""),
    (["--validate", ""], b""),
    (["--validate", "Zm9vY"], b""),
    (["--validate", "Zm9v!"], b""),
    (["--validate"], b"Zm9v\nYmFy\n"),
    (["-d", "--", "-_-_"], b""),
    (["-d"], b"Zm9v\nYmFy\n"),
    (["-d"], b"  Zm9v\tYmFy\r\n"),
    (["-d", "data:text/plain;base64,SGk="], b""),
    (["-d", ""], b""),
    (["-d", "Zg="], b""),
    (["-d", "Zm9v!"], b""),
    (["-d", "Zm=9v"], b""),
    (["-d", "Zg==="], b""),
    (["-d", "Zm9vY"], b""),
    (["-d", "Zm9v!=YmFy"], b""),
    (["-d", "-u", "Zm9v"], b""),
    (["-d", "-n", "Zm9v"], b""),
    (["-d", "-w", "4", "Zm9v"], b""),
    (["-d", "--data-uri", "Zm9v"], b""),
    (["-d", "--validate", "Zm9v"], b""),
    (["--validate", "-u", "Zm9v"], b""),
    (["-m", "text/plain", "x"], b""),
    (["--data-uri", "-u", "x"], b""),
    (["--data-uri", "-n", "x"], b""),
    (["--data-uri", "-w", "4", "x"], b""),
    (["-dn", "x"], b""),
    (["-w76", "x"], b""),
    (["--bogus"], b""),
    (["-x"], b""),
    (["a", "b"], b""),
    (["-w", "-3", "x"], b""),
    (["-w", "abc", "x"], b""),
    (["-w"], b""),
    (["--decode=1", "x"], b""),
    (["-i", "missing.txt", "x"], b""),
    (["-v"], b""),
    (["--version"], b""),
]

def main(binary):
    out = []
    for args, stdin in CASES:
        p = subprocess.run([binary] + args, input=stdin, capture_output=True, env={"LC_ALL": "C.UTF-8"})
        out.append({"args": args, "stdin_hex": stdin.hex(), "stdout_hex": p.stdout.hex(),
                    "stderr": p.stderr.decode(), "exit": p.returncode})
    here = os.path.dirname(os.path.abspath(__file__))
    with open(os.path.join(here, "..", "testdata", "cli.json"), "w") as f:
        json.dump({"version": "1.0.0", "cases": out}, f, ensure_ascii=False, indent=1)
        f.write("\n")

if __name__ == "__main__":
    main(sys.argv[1])
