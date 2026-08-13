export interface ProjectEnvResult {
  path: string
  loaded: boolean
  applied: string[]
}

export interface ProjectEnvOptions {
  root?: string
  cwd?: string
}

export function loadProjectEnv(environment?: NodeJS.ProcessEnv, options?: ProjectEnvOptions): ProjectEnvResult
