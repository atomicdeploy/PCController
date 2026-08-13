export interface ProjectEnvResult {
  path: string
  loaded: boolean
  applied: string[]
}

export interface ProjectEnvOptions {
  root?: string
  cwd?: string
}

export function parseEnvFile(content: string, source?: string): Map<string, string>
export function resolveProjectEnvFile(environment?: NodeJS.ProcessEnv, options?: ProjectEnvOptions): string
export function loadProjectEnv(environment?: NodeJS.ProcessEnv, options?: ProjectEnvOptions): ProjectEnvResult
export const repositoryRoot: string
