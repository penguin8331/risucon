# RISUCON

## 初期データについて

### `users, teams`

| username | password | 備考            |
| -------- | -------- | --------------- |
| admin    | admin    | 管理者          |
| risucon1 | risucon1 | team1 リーダー  |
| risucon2 | risucon2 | team1 メンバー1 |
| risucon3 | risucon3 | team1 メンバー2 |
| risucon4 | risucon4 | team2 リーダー  |

## GCE 上での操作

### VM の作成

| 項目                 | 値（test）                     |
| -------------------- | ------------------------------ |
| ゾーン               | asia-northeast1-a              |
| マシンタイプ         | e2-medium                      |
| OS                   | Debian GNU/Linux 12 (bookworm) |
| ブートディスクサイズ | 20GB                           |
| tags                 | http-server, http-8080         |

### ファイアーウォールルールを作成

| 項目               | 値                  |
| ------------------ | ------------------- |
| トラフィックの方向 | 上り（Ingress）     |
| ターゲットタグ     | http-8080           |
| ソース IP          | 0.0.0.0/0（全許可） |
| プロトコルとポート | TCP: 8080           |

### Docker のインストール

<https://docs.docker.com/engine/install/debian/#install-using-the-repository>

```sh
## 1. Set up Docker's `apt` repository.
# Add Docker's official GPG key:
sudo apt update
sudo apt install ca-certificates curl
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc
# Add the repository to Apt sources:
sudo tee /etc/apt/sources.list.d/docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/debian
Suites: $(. /etc/os-release && echo "$VERSION_CODENAME")
Components: stable
Signed-By: /etc/apt/keyrings/docker.asc
EOF

sudo apt update

## 2. Install the Docker packages.
sudo apt install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
```

### nginx の構成

```sh
bash ./nginx/gen-certs.sh <your-ip-address>
docker compose -f docker-compose.prod.yml up -d
```
