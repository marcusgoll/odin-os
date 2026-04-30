export interface SecurityDecision {
  readonly allowed: boolean;
  readonly reason: string;
}

export function denyProductionSecretAccess(path: string): SecurityDecision {
  const normalized = path.toLowerCase();
  const denied = normalized.includes("prod") || normalized.includes(".env");

  return {
    allowed: !denied,
    reason: denied ? "production_secret_or_env_path_denied" : "allowed"
  };
}
