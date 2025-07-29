# AI/LLM contribution guidelines

- Because of the still unclear copyright side of AI contributions, the maintainers of the project have the
  duty and reserve the option to reject AI-assisted contributions if the legal/copyright side is deemed not
  adequate for the project. We acknowledge this is unfortunate and we seek to clarify and streamline as soon
  as possible, but it is likely the legal framework will take unpredictable amount of time to solidify.
- The project assumes human contributions by default. If not explicitly marked, a contribution is assumed
  to have been made without any AI assistance. It is the responsibility of the owner of the change to
  mark the AI-assisted contributions correctly according to the instructions documented in the rest of the
  document.
- AI-assisted contributions should always be marked as so, as documented in the rest of the document.
- AI-assisted commits (each of them) _and_ PRs must have the `AA` tag (AI-Assisted) in their subject line.
- AI-assisted commits must include the `Assisted-by` tag in the bottom section, alongside the already mandatory
  `Signed-off-by` tag (DCO). The value of the tag must include the AI assistant being used and its version.
  Example: `Assisted-by: Cursor v1.2.2`
- AI-assisted commits must include the `AI-Attribution` tag in the bottom section. The tag must be an
  Abbreviated Statement in the https://aiattribution.github.io/ format.
  We acknowledge, this duplicates the `Assisted-by` content, but is necessary for logistic reasons.
- AI-assisted commits should adhere to the project rules outlined in `PROMPTING.md`
