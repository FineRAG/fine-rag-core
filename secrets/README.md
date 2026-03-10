# Docker Secrets

Create the following files locally before starting the stack:

- `postgres_user.txt`
- `postgres_password.txt`
- `postgres_db.txt`
- `minio_root_user.txt`
- `minio_root_password.txt`
- `grafana_admin_user.txt`
- `grafana_admin_password.txt`
- `finerag_jwt_secret.txt`
- `finerag_database_url.txt`
- `finerag_minio_access_key.txt`
- `finerag_minio_secret_key.txt`
- `finerag_bootstrap_admin_username.txt`
- `finerag_bootstrap_admin_password.txt`
- `finerag_bootstrap_admin_api_key.txt`
- `finerag_portkey_api_key.txt`
- `finerag_openrouter_api_key.txt`
- `finerag_milvus_endpoint.txt`
- `finerag_milvus_username.txt`
- `finerag_milvus_password.txt`
- `finerag_milvus_token.txt`

Example DB URL format for `finerag_database_url.txt`:

`postgres://<user>:<password>@postgres:5432/<db>?sslmode=disable`

Example Milvus endpoint format for `finerag_milvus_endpoint.txt`:

`<host>:443`
