# WARNING

> **Shmorby is an autonomous AI sysadmin agent with full shell, SSH, sudo,**
> **and AWS CLI access.** It can execute arbitrary commands on your
> infrastructure without human intervention depending on your permission
> configuration.

## Dangers

- **Data loss** — Shmorby may delete files, databases, or entire
  filesystems if instructed or misled.
- **Service disruption** — It can restart, reconfigure, or take down
  production services.
- **Security exposure** — Commands may inadvertently open ports, modify
  firewall rules, expose credentials, or weaken system security.
- **Credential leakage** — API keys, SSH keys, and AWS credentials in
  scope may be read or exfiltrated.
- **Prompt injection** — Untrusted input (logs, web content, chat
  messages, tool output) can influence Shmorby's behavior and cause it
  to take unintended actions.
- **Cascade failures** — A single mistaken command can trigger a chain
  of automated remediation attempts that compound the damage.
- **Misjudgment** — LLMs can misinterpret intent, misidentify resources,
  or hallucinate commands that do something other than what was asked.

## Mitigations

- Run Shmorby in a non-production environment first.
- Use the **diagnose** mode for read-only inspection.
- Set permission presets to **read-only** or **locked** when not actively
  operating.
- Configure tools like `sudo` and `aws` to **ask** (require approval).
- Never give Shmorby access to systems you are not prepared to lose.
- Audit all configuration and SCOPE.md contents before granting access.

## Disclaimer

THIS SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS
OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

**The author(s) of this software are not responsible for any damage,
data loss, service disruption, security breach, or other harm resulting
from the use of Shmorby.** You alone bear the risk of running autonomous
AI agents on your infrastructure.

See the [GNU General Public License v3](LICENSE) for the full terms
under which this software is distributed.
