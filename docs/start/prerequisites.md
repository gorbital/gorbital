# What you need

Install these once, before the [Quickstart](quickstart.md). Each entry says what the tool is, why gorbital needs it, how to install it on macOS and Linux, and how to check it works.

| Tool | Needed for | Version |
|---|---|---|
| [Go](#go) | Building and running your app and `orb` | 1.26 or later, latest patch |
| [Docker](#docker) | PostgreSQL and Mailpit on your computer | Docker Engine with Compose v2 |
| [git](#git) | `orb` commands that change your app | Any recent version |
| [An authenticator app](#an-authenticator-app) | Signing in as the administrator | Any |
| [curl and jq](#curl-and-jq) | Trying the API from a terminal | Optional |
| [openssl](#openssl) | Generating production keys | Optional; usually already installed |

The Minimal preset only needs Go. Everything else is for the Full preset.

## Go

**What it is:** the programming language your API is written in, with its compiler and tools.

**Why you need it:** `orb` is a Go program, and your app is built with `go build` and `go run`. There is no other runtime.

**Version:** Go **1.26 or later**. Use the latest patch release (such as 1.26.8 rather than 1.26.0): early patch releases have known security fixes missing.

**Install on macOS:** download the installer from [go.dev/dl](https://go.dev/dl/) and open it, or with [Homebrew](https://brew.sh):

```bash
brew install go
```

**Install on Linux:** download the archive for your processor from [go.dev/dl](https://go.dev/dl/), then follow the [official instructions](https://go.dev/doc/install): remove any old `/usr/local/go`, extract the archive there, and add `/usr/local/go/bin` to your `PATH`. Distribution packages (`apt install golang`) are often too old.

**Check it:**

```bash
go version
```

```text
go version go1.26.8 darwin/arm64
```

Then make sure programs installed with `go install` can be found. Add this line to `~/.zshrc` (macOS) or `~/.bashrc` (Linux), and open a new terminal:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

## Docker

**What it is:** software that runs other programs in isolated containers, from ready-made images, without installing them on your computer.

**Why you need it:** a Full app needs PostgreSQL (its database) and Mailpit (a local inbox for its emails). `orb dev` runs both in Docker from your app's `compose.yaml`. gorbital never installs PostgreSQL on your computer, so every developer and every test uses the same version.

**Version:** a current Docker Desktop, or Docker Engine with the **Compose v2** plugin (the `docker compose` command, with a space).

**Install on macOS:** download [Docker Desktop](https://www.docker.com/products/docker-desktop/) and open it. [OrbStack](https://orbstack.dev) and [Colima](https://github.com/abiosoft/colima) also work.

**Install on Linux:** follow [Install Docker Engine](https://docs.docker.com/engine/install/) for your distribution, which includes the Compose plugin. Then let your user run Docker without `sudo` ([post-installation steps](https://docs.docker.com/engine/install/linux-postinstall/)).

**Check it:** Docker must be **running** (on macOS, open Docker Desktop first), then:

```bash
docker version
docker compose version
docker run --rm hello-world
```

`docker compose version` should print `Docker Compose version v2.…`. If `docker version` shows a client but says it can't connect to the daemon, Docker isn't running.

## git

**What it is:** version control.

**Why you need it:** `orb new` creates a git repository for your app, and `orb gen`, `orb add` and `orb upgrade` refuse to run on uncommitted changes, so every generated change is a diff you can review.

**Install on macOS:** `xcode-select --install`, or `brew install git`. **On Linux:** `sudo apt install git`, `sudo dnf install git`, or your distribution's equivalent.

**Check it:**

```bash
git --version
```

## An authenticator app

**What it is:** an app that shows 6-digit codes that change every 30 seconds, such as Google Authenticator, Microsoft Authenticator, 1Password, Bitwarden or Authy.

**Why you need it:** your app's administrator role requires two-factor authentication. The first `orb dev` prints the administrator's 2FA key; you add it to the app to get codes.

## curl and jq

**What they are:** `curl` sends HTTP requests from a terminal; `jq` reads values out of JSON.

**Why:** the guides show API calls with `curl` and pick values like tokens out of replies with `jq`. You can use your app's `/docs` page instead, which has a **Try it** button on every endpoint.

**Install:** `curl` is already on macOS and most Linux systems. `jq`: `brew install jq` on macOS, `sudo apt install jq` on Debian and Ubuntu.

**Check them:** `curl --version` and `jq --version`.

## openssl

**What it is:** a toolkit for cryptography.

**Why:** the command that generates a production encryption key uses it: `openssl rand -base64 32`. On your computer, `orb dev` generates the development key for you.

**Check it:** `openssl version`. It's part of macOS and nearly every Linux system.

## Ports

`orb dev` uses these ports on your computer. Something else already using one is the most common first-run problem; the [Quickstart](quickstart.md#if-a-port-is-taken) shows how to move each.

| Port | Used by |
|---|---|
| 8080 | Your API |
| 5432 | PostgreSQL |
| 1025 | Mailpit, receiving email |
| 8025 | Mailpit's inbox in your browser |
| 3000 and 4318 | Grafana, only with `orb dev --observability` |

To see what's using a port on macOS or Linux:

```bash
lsof -nP -iTCP:5432 -sTCP:LISTEN
```
