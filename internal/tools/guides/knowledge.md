# Knowledge base

| Kind | Use for | Tool |
|---|---|---|
| Crawled pages | Public docs, help centre, pricing, policies | `crawl_website`, then `get_crawl_job`; `schedule_website_crawl` to keep it fresh |
| Reference document | Facts not on the site, pasted text | `add_knowledge_document` (default kind) |
| FAQ | One short question and its answer | `add_faq` |
| Instruction document | A topic playbook the assistant follows ("refund dispute: ask for the order number, check getOrder, …"). Retrieved only on turns about that topic; max 20,000 characters, but only the best-matching ~600 tokens are used per turn | `add_knowledge_document` with `kind=instruction` |

Tips:
- One topic per document, with a clear title. Retrieval works on chunks, so a page that mixes ten topics matches poorly.
- Write the words customers use ("cancel", "refund my money"), not only internal names.
- Cover each language your customers write in if the content differs per market.
- Check with `search_knowledge_base` (what retrieval finds) and `test_assistant` (what the assistant answers). Fill gaps with knowledge, not longer instructions.
- Remove stale documents with `delete_knowledge_document` (confirm first).
