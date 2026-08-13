export type SessionStreamState = 'connecting' | 'open' | 'waiting' | 'closed'

interface AuthenticationGuidanceInput {
  hostRequiresAuthentication: boolean
  streamState: SessionStreamState
  token: string
  streamDetail?: string
  connectionReason?: string
}

const authenticationFailure = /\b(?:http\s*)?(?:401|403)\b|unauthori[sz]ed|authentication (?:required|rejected|failed)|invalid (?:session )?token|token (?:is )?(?:missing|invalid|rejected|required|expired)/i

export function sessionAuthenticationGuidanceRequired(input: AuthenticationGuidanceInput): boolean {
  if (!input.hostRequiresAuthentication || input.streamState === 'open') return false
  if (!input.token.trim()) return true
  return authenticationFailure.test(`${input.streamDetail ?? ''} ${input.connectionReason ?? ''}`)
}
