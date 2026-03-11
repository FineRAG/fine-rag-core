# Docker Secrets

Create the following files locally before starting the stack:

- `postgres_user.txt`
- `postgres_password.txt`
- `postgres_db.txt`
- `finerag_jwt_secret.txt`
- `finerag_database_url.txt`
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

For AWS S3-backed deployments, the backend now prefers the default AWS credential chain.
On EC2, attach an IAM role with S3 access to the instance and set these environment variables in the runtime:

- `FINE_RAG_S3_BUCKET`
- `FINE_RAG_S3_REGION`

Optional S3 overrides for non-AWS or local S3-compatible stores:

- `FINE_RAG_S3_ENDPOINT`
- `FINE_RAG_S3_ACCESS_KEY`
- `FINE_RAG_S3_SECRET_KEY`
- `FINE_RAG_S3_USE_PATH_STYLE`
- `FINE_RAG_UPLOAD_PUBLIC_BASE_URL`
