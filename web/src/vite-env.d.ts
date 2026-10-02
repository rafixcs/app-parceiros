interface ImportMetaEnv {
  readonly VITE_AUTH_MODE?: "dev" | "oidc";
  readonly VITE_OIDC_ISSUER: string;
  readonly VITE_OIDC_CLIENT_ID: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
