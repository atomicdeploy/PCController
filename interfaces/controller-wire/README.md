# Stable controller opcode ownership

Project/ProtocolContract.json is the source owner for the native and Pakar
namespaces. registry.mjs is the portable implementation adapted from the existing
PCController generator; the original build entrypoint remains its thin wrapper.
Numeric opcode names, C++/Go views, Rust/Dart views and Go log names are generated.
Never add a hand-maintained numeric opcode table in a consumer.

Published assignments are append-only. published.json is an independent golden
compatibility fixture, not generator output. CI also checks every existing
assignment against the actual pull-request/push base so changing the fixture
and regenerating cannot authorize incompatible renumbering or removal.
Keep retired allocations reserved; advertise optional support using capabilities.

Native frames retain A5/1 and Pakar frames retain A5/0. Those fixed discriminator
bytes are namespace admission, not versions. Every decoder must reject another
namespace before semantic dispatch. Unknown operations within the admitted
namespace remain Unsupported; never reinterpret them through another dispatcher.
The existing explicit Pakar PC-contract tunnel remains the only such crossing.
CRC detects corruption and is not authentication or command authorization.

After committing canonical sources, use Tools/Build/sync-controller-opcodes.mjs
with --pakar-root PATH or --pealayer-root PATH. It preserves modified imported
sources rather than overwriting a draft. Each consumer receives exact source
hashes and the immutable owner commit, then runs its consumer.mjs with --pakar
or --pealayer. --check never writes. --upstream additionally verifies pinned bytes
against that exact public GitHub commit without executing downloaded code.

Source/fixture checks do not prove deployed firmware, electrical output, timing
or hardware interoperability. See docs/Controller-Convergence.md and the linked
cross-repository issues for remaining transport and MCU acceptance.
