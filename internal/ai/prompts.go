package ai

// The prompts in this file encode the editorial guardrails from §21–§25.
// Two rules are repeated in every prompt because they are the ones that matter
// most: the model works only from facts the journalist supplied, and nothing it
// produces reaches readers without a human editor.

// BreakingNewsPrompt is the draft prompt specified verbatim in §22. The
// placeholders are filled by fillBreakingPrompt.
const BreakingNewsPrompt = `Act as a senior Cambodian news editor for Cambodia Fast News.

Create a concise breaking-news draft using ONLY the verified information provided.

TOPIC:
{{TOPIC}}

VERIFIED FACTS:
{{FACTS}}

SOURCE:
{{SOURCE}}

DATE:
{{DATE}}

Return:

{
  "titleKh": "",
  "titleEn": "",
  "category": "",
  "subCategory": "",
  "summary": "",
  "paragraph1": "",
  "paragraph2": "",
  "telegramPost": ""
}

Rules:

Do not invent facts.
Do not invent quotes.
Do not invent statistics.
Clearly attribute claims.
Use natural professional Khmer.
Keep the headline concise.`

// systemPrompt is prepended to every assistant call. It states the boundary the
// product depends on: this model drafts, it does not publish.
const systemPrompt = `You are a drafting assistant inside the Cambodia Fast News newsroom CMS.

Your output is always an unpublished draft that a human editor reviews before
anything reaches readers. You never publish.

Absolute rules:
- Work ONLY from the verified information the journalist gives you. If a detail
  is not in that information, leave it out.
- Never invent quotes, statistics, names, events, dates, locations or sources.
- If the supplied facts are too thin to support the requested output, say so in
  the "editorNote" field rather than filling the gap.
- Attribute every claim to the source the journalist provided.
- Write natural, professional Khmer for Khmer fields. Do not translate
  word-for-word from English; write as a Cambodian editor would write.
- Return only the requested JSON object, with no markdown fence and no
  commentary outside it.`

// SEOPrompt generates the metadata fields in §23. Keyword stuffing is called
// out explicitly because it is the default failure mode for this task.
const SEOPrompt = `Generate search metadata for this Cambodia Fast News article.

HEADLINE (Khmer):
{{TITLE_KH}}

HEADLINE (English):
{{TITLE_EN}}

SUMMARY:
{{SUMMARY}}

ARTICLE TEXT:
{{BODY}}

CATEGORY:
{{CATEGORY}}

Return this JSON object:

{
  "seoTitle": "",
  "seoDescription": "",
  "suggestedSlug": "",
  "ogTitle": "",
  "ogDescription": "",
  "imageAlt": "",
  "keywords": [],
  "internalLinkSuggestions": [],
  "editorNote": ""
}

Rules:

Write seoTitle at most 60 characters and seoDescription at most 155 characters.
Describe what the article actually says — never promise more than the text delivers.
suggestedSlug must be lowercase ASCII words separated by hyphens, derived from the English headline.
Do not repeat a keyword to game ranking. Natural phrasing only.
keywords: at most 8 phrases a reader would plausibly search.
internalLinkSuggestions: topics this article should link to, not invented URLs.
Return only the JSON object.`

// SummaryPrompt backs the "⚡ សង្ខេបព័ត៌មាន" block in §24. The summary is
// rendered under its own labelled heading, never merged into the article body.
const SummaryPrompt = `Summarise this Cambodia Fast News article into key points for readers in a hurry.

HEADLINE:
{{TITLE}}

ARTICLE TEXT:
{{BODY}}

Return this JSON object:

{
  "points": [],
  "editorNote": ""
}

Rules:

Write 3 to 5 points, in Khmer, each one sentence.
Every point must be supported by the article text above. Add nothing new.
Do not include a point that the article only implies.
If the article is too short to need a summary, return an empty points array and explain in editorNote.
Return only the JSON object.`

// ImagePrompt is one entry in the internal prompt library from §25.
type ImagePrompt struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Category    string `json:"category"`
	Prompt      string `json:"prompt"`
	AspectRatio string `json:"aspectRatio"`
}

// ImagePromptLibrary is the editorial prompt set from §25.
//
// Images produced from these prompts are illustrations, not photographs of a
// real event. Anything uploaded from here is stored with IsAIGenerated set, and
// the article page labels it — an AI image must never be presented as
// documentary evidence of a breaking event.
func ImagePromptLibrary() []ImagePrompt {
	return []ImagePrompt{
		{
			Key: "phnom-penh", Label: "Phnom Penh", Category: "phnom-penh", AspectRatio: "16:9",
			Prompt: "A high-resolution editorial photograph of Phnom Penh's modern skyline with realistic Cambodian urban traffic, documentary photography style, natural lighting, professional journalism photography, 16:9.",
		},
		{
			Key: "kun-khmer", Label: "Kun Khmer", Category: "kun-khmer", AspectRatio: "16:9",
			Prompt: "Dynamic editorial sports photograph of Kun Khmer fighters competing inside a professional Cambodian stadium, realistic lighting, documentary sports photography, 16:9.",
		},
		{
			Key: "business", Label: "Business", Category: "business", AspectRatio: "16:9",
			Prompt: "Professional editorial photograph representing Cambodia's modern economy and business environment, realistic Phnom Penh setting, documentary photography, 16:9.",
		},
		{
			Key: "breaking-graphic", Label: "Breaking News Graphic", Category: "breaking", AspectRatio: "16:9",
			Prompt: "Modern breaking-news newsroom graphic using deep navy blue #1e3a8a, Cambodia-inspired visual elements, digital data network, professional broadcast style, 16:9.",
		},
	}
}

// AIImageDisclosure is the label the frontend renders over any image whose
// IsAIGenerated flag is set.
const AIImageDisclosure = "រូបភាពបង្កើតដោយ AI សម្រាប់ជាឧទាហរណ៍ (AI-generated illustration)"
