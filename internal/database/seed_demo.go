package database

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/utils"
)

// Demo content exists so a fresh install shows a working newsroom instead of
// empty sections. It is created only when Options.Demo is set, and every record
// is keyed on a stable slug so re-running changes nothing.
//
// Images are inline SVG data URIs rather than links to a placeholder service:
// they render offline, never 404, and make it obvious at a glance that this is
// sample content.

// Demo imagery
//
// Article and video images come from Lorem Picsum, which serves Unsplash
// photographs under a licence that permits this use. Seeding by slug makes
// each article keep the same photo across reseeds.
//
// These are decorative placeholders, not pictures of the events the demo
// headlines describe, so every demo article carries a caption saying so. The
// same rule that governs AI imagery applies here: a photograph must never be
// presented as documentary evidence of something it does not show.
//
// Using photographs from a real newsroom would infringe their copyright and
// would put their journalism behind headlines they never published.
//
// Set SEED_IMAGE_SOURCE=svg for self-contained inline graphics that need no
// network — useful for CI, air-gapped installs, or when the demo must not
// make outbound requests.

// Image columns are 512 characters, so the SVG variants are kept deliberately
// small. seed_test.go asserts they fit — a placeholder that overflows the
// column fails the whole seed with an opaque MySQL error.
const maxImageURLLen = 512

// demoImageCaption is attached to every demo article so the placeholder is
// never mistaken for reporting.
const demoImageCaption = "រូបភាពគំរូ — មិនមែនជារូបថតនៃព្រឹត្តិការណ៍នេះទេ (placeholder image, not a photograph of this event)"

// articleImage returns the 16:9 image for a demo article or video.
func (s *seeder) articleImage(seed, label, colour string) string {
	if s.usePhotos() {
		return photoURL(seed, 1200, 675)
	}
	return placeholderImage(label, colour)
}

func (s *seeder) usePhotos() bool {
	return s.cfg == nil || strings.ToLower(s.cfg.SeedImageSource) != "svg"
}

// photoURL builds a deterministic Lorem Picsum URL. The seed keeps a given
// article on the same photograph between reseeds.
func photoURL(seed string, width, height int) string {
	return fmt.Sprintf("https://picsum.photos/seed/%s/%d/%d", url.PathEscape(seed), width, height)
}

// placeholderImage builds a 16:9 branded placeholder for a demo article.
func placeholderImage(label, colour string) string {
	return dataURI(fmt.Sprintf(
		`<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 1200 675'>`+
			`<rect width='1200' height='675' fill='%s'/>`+
			`<text x='600' y='390' font-size='150' text-anchor='middle' fill='#fff' opacity='.35'>%s</text>`+
			`</svg>`, colour, label))
}

// avatarImage builds a square initial for a demo author byline.
//
// Bylines deliberately never use a stock photograph: attaching a real person's
// face to an invented journalist fabricates an identity, which is a different
// and worse thing than a decorative article image.
func avatarImage(initial, colour string) string {
	return dataURI(fmt.Sprintf(
		`<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 96 96'>`+
			`<rect width='96' height='96' fill='%s'/>`+
			`<text x='48' y='63' font-size='42' font-family='sans-serif' text-anchor='middle' fill='#fff'>%s</text>`+
			`</svg>`, colour, initial))
}

// dataURI percent-encodes an SVG for use in an img src.
func dataURI(svg string) string {
	return "data:image/svg+xml;charset=utf-8," + url.PathEscape(svg)
}

// ── Authors ─────────────────────────────────────────────────────────────

var demoAuthorSeeds = []models.Author{
	{
		Slug: "sok-dara", NameKh: "សុខ ដារា", NameEn: "Sok Dara",
		Title: "អ្នកយកព័ត៌មានជាន់ខ្ពស់ផ្នែកនយោបាយ",
		BioKh: "សុខ ដារា រាយការណ៍អំពីនយោបាយ និងគោលនយោបាយសាធារណៈនៅកម្ពុជា អស់រយៈពេលជាងដប់ឆ្នាំ។",
	},
	{
		Slug: "chan-sophea", NameKh: "ចាន់ សុភា", NameEn: "Chan Sophea",
		Title: "អ្នកយកព័ត៌មានសេដ្ឋកិច្ច",
		BioKh: "ចាន់ សុភា តាមដានវិស័យធនាគារ វិនិយោគ និងពាណិជ្ជកម្មក្នុងតំបន់។",
	},
	{
		Slug: "vann-pisey", NameKh: "វណ្ណ ពិសី", NameEn: "Vann Pisey",
		Title: "អ្នកយកព័ត៌មានកីឡា",
		BioKh: "វណ្ណ ពិសី គ្របដណ្តប់លើគុនខ្មែរ បាល់ទាត់ជាតិ និងព្រឹត្តិការណ៍កីឡាក្នុងតំបន់។",
	},
	{
		Slug: "kim-sreyneath", NameKh: "គឹម ស្រីណាត", NameEn: "Kim Sreyneath",
		Title: "អ្នកយកព័ត៌មានបច្ចេកវិទ្យា និងជីវិត",
		BioKh: "គឹម ស្រីណាត សរសេរអំពីបច្ចេកវិទ្យា ស្ទាតជំនាញឌីជីថល និងជីវិតទីក្រុង។",
	},
}

func (s *seeder) demoAuthors() (int, int, error) {
	created, existing := 0, 0
	for _, seed := range demoAuthorSeeds {
		record := seed
		record.IsActive = true
		record.PhotoURL = avatarImage(string([]rune(seed.NameEn)[:1]), "#1E3A8A")

		result := s.db.Where("slug = ?", seed.Slug).Attrs(record).FirstOrCreate(&record)
		if result.Error != nil {
			return created, existing, result.Error
		}
		if result.RowsAffected > 0 {
			created++
		} else {
			existing++
		}
	}
	return created, existing, nil
}

// ── Articles ────────────────────────────────────────────────────────────

// demoArticleSeed describes one sample story. MinutesAgo positions it on the
// timeline so the homepage has a believable spread rather than everything
// carrying the same timestamp.
type demoArticleSeed struct {
	Slug        string
	TitleKh     string
	TitleEn     string
	SummaryKh   string
	Body        []string
	Category    string
	AuthorSlug  string
	MinutesAgo  int
	Views       int64
	IsBreaking  bool
	IsFeatured  bool
	ContentType models.ContentType
	Sponsor     string
}

var demoArticleSeeds = []demoArticleSeed{
	{
		Slug: "phnom-penh-metro-feasibility-study", TitleKh: "ការសិក្សាលទ្ធភាពគម្រោងរថភ្លើងក្រោមដីភ្នំពេញ ចាប់ផ្តើមដំណាក់កាលទីពីរ",
		TitleEn: "Phnom Penh metro feasibility study enters second phase",
		SummaryKh: "ក្រុមការងារបច្ចេកទេសបានចាប់ផ្តើមវាយតម្លៃផ្លូវដែលអាចធ្វើទៅបាន បន្ទាប់ពីបញ្ចប់ការសិក្សាដំណាក់កាលដំបូង។",
		Body: []string{
			"ក្រុមការងារបច្ចេកទេសបានចាប់ផ្តើមដំណាក់កាលទីពីរនៃការសិក្សាលទ្ធភាព សម្រាប់គម្រោងប្រព័ន្ធដឹកជញ្ជូនសាធារណៈក្នុងរាជធានីភ្នំពេញ។",
			"ដំណាក់កាលនេះផ្តោតលើការវាយតម្លៃផ្លូវដែលអាចធ្វើទៅបាន តម្លៃសាងសង់ និងផលប៉ះពាល់លើចរាចរណ៍ក្នុងអំឡុងពេលសាងសង់។",
			"លទ្ធផលនៃការសិក្សានេះ ត្រូវបានរំពឹងថានឹងបញ្ចប់នៅចុងឆ្នាំក្រោយ មុននឹងមានការសម្រេចចិត្តណាមួយ។",
		},
		Category: "phnom-penh", AuthorSlug: "sok-dara", MinutesAgo: 14, Views: 4820, IsBreaking: true, IsFeatured: true,
	},
	{
		Slug: "garment-exports-rise-third-quarter", TitleKh: "ការនាំចេញវិស័យកាត់ដេរកើនឡើងក្នុងត្រីមាសទីបី",
		TitleEn: "Garment exports rise in the third quarter",
		SummaryKh: "តួលេខផ្លូវការបង្ហាញពីការកើនឡើងនៃការនាំចេញ ខណៈវិស័យនេះនៅតែជាប្រភពការងារធំបំផុតមួយ។",
		Body: []string{
			"ទិន្នន័យផ្លូវការដែលចេញផ្សាយសប្តាហ៍នេះ បង្ហាញពីការកើនឡើងនៃការនាំចេញផលិតផលកាត់ដេរក្នុងត្រីមាសទីបី។",
			"វិស័យកាត់ដេរនៅតែជាប្រភពការងារធំបំផុតមួយរបស់ប្រទេស ជាពិសេសសម្រាប់កម្មករស្ត្រី។",
			"អ្នកវិភាគបាននិយាយថា និន្នាការនេះអាស្រ័យលើតម្រូវការពីទីផ្សារនាំចូលធំៗ។",
		},
		Category: "business", AuthorSlug: "chan-sophea", MinutesAgo: 47, Views: 2310, IsFeatured: true,
	},
	{
		Slug: "kun-khmer-national-championship-finals", TitleKh: "ការប្រកួតវគ្គផ្តាច់ព្រ័ត្រជើងឯកគុនខ្មែរជាតិ នឹងប្រព្រឹត្តទៅសប្តាហ៍ក្រោយ",
		TitleEn: "Kun Khmer national championship finals set for next week",
		SummaryKh: "អ្នកប្រដាល់ប្រាំបីនាក់បានឆ្លងចូលដល់វគ្គផ្តាច់ព្រ័ត្រ បន្ទាប់ពីការប្រកួតវគ្គពាក់កណ្តាលផ្តាច់ព្រ័ត្រ។",
		Body: []string{
			"អ្នកប្រដាល់គុនខ្មែរចំនួនប្រាំបីនាក់ បានឆ្លងចូលដល់វគ្គផ្តាច់ព្រ័ត្រនៃការប្រកួតជើងឯកជាតិ។",
			"ការប្រកួតនឹងប្រព្រឹត្តទៅនៅសប្តាហ៍ក្រោយ ហើយនឹងត្រូវផ្សាយផ្ទាល់។",
			"គុនខ្មែរនៅតែជាកីឡាប្រពៃណីដ៏ពេញនិយមបំផុតមួយរបស់កម្ពុជា។",
		},
		Category: "kun-khmer", AuthorSlug: "vann-pisey", MinutesAgo: 95, Views: 6140, IsFeatured: true,
	},
	{
		Slug: "digital-payment-adoption-grows", TitleKh: "ការប្រើប្រាស់ការទូទាត់ឌីជីថលកើនឡើងក្នុងចំណោមអាជីវកម្មខ្នាតតូច",
		TitleEn: "Digital payment adoption grows among small businesses",
		SummaryKh: "ការស្ទង់មតិថ្មីបង្ហាញថា អាជីវកម្មខ្នាតតូចកាន់តែច្រើនកំពុងទទួលយកការទូទាត់តាមទូរស័ព្ទ។",
		Body: []string{
			"ការទូទាត់តាមកូដ QR កាន់តែក្លាយជារឿងធម្មតានៅតាមផ្សារ និងហាងលក់រាយក្នុងទីក្រុង។",
			"ម្ចាស់អាជីវកម្មបាននិយាយថា វាកាត់បន្ថយតម្រូវការក្នុងការគ្រប់គ្រងសាច់ប្រាក់។",
			"ទោះជាយ៉ាងណា ការភ្ជាប់អ៊ីនធឺណិតនៅតំបន់ជនបទនៅតែជាឧបសគ្គមួយ។",
		},
		Category: "technology", AuthorSlug: "kim-sreyneath", MinutesAgo: 140, Views: 1890,
	},
	{
		Slug: "regional-summit-trade-talks", TitleKh: "កិច្ចប្រជុំកំពូលតំបន់ផ្តោតលើកិច្ចពិភាក្សាពាណិជ្ជកម្ម",
		TitleEn: "Regional summit focuses on trade talks",
		SummaryKh: "តំណាងមកពីប្រទេសជាច្រើនបានជួបគ្នា ដើម្បីពិភាក្សាអំពីកិច្ចសហប្រតិបត្តិការពាណិជ្ជកម្ម។",
		Body: []string{
			"កិច្ចប្រជុំកំពូលតំបន់បានចាប់ផ្តើមនៅថ្ងៃនេះ ដោយផ្តោតលើកិច្ចពិភាក្សាពាណិជ្ជកម្ម និងការតភ្ជាប់។",
			"របៀបវារៈរួមមានការសម្របសម្រួលពិធីការគយ និងការវិនិយោគលើហេដ្ឋារចនាសម្ព័ន្ធ។",
		},
		Category: "world", AuthorSlug: "sok-dara", MinutesAgo: 210, Views: 1240,
	},
	{
		Slug: "national-football-team-friendly", TitleKh: "ក្រុមបាល់ទាត់ជម្រើសជាតិ រៀបចំសម្រាប់ការប្រកួតមិត្តភាព",
		TitleEn: "National football team prepares for friendly match",
		SummaryKh: "ក្រុមជម្រើសជាតិបានចាប់ផ្តើមការហ្វឹកហាត់ មុនការប្រកួតមិត្តភាពនៅចុងខែនេះ។",
		Body: []string{
			"ក្រុមបាល់ទាត់ជម្រើសជាតិបានចាប់ផ្តើមជំរុំហ្វឹកហាត់នៅរាជធានីភ្នំពេញ។",
			"គ្រូបង្វឹកបាននិយាយថា ការប្រកួតនេះជាឱកាសសម្រាប់សាកល្បងកីឡាករវ័យក្មេង។",
		},
		Category: "sports", AuthorSlug: "vann-pisey", MinutesAgo: 280, Views: 3420,
	},
	{
		Slug: "new-public-library-opens", TitleKh: "បណ្ណាល័យសាធារណៈថ្មីបើកដំណើរការនៅរាជធានីភ្នំពេញ",
		TitleEn: "New public library opens in Phnom Penh",
		SummaryKh: "បណ្ណាល័យនេះផ្តល់កន្លែងអានសៀវភៅ និងកម្មវិធីអប់រំសម្រាប់សិស្សានុសិស្ស។",
		Body: []string{
			"បណ្ណាល័យសាធារណៈថ្មីមួយបានបើកដំណើរការ ដោយផ្តល់កន្លែងអានសៀវភៅ និងបន្ទប់សិក្សា។",
			"អ្នករៀបចំបាននិយាយថា កម្មវិធីអប់រំសម្រាប់កុមារនឹងចាប់ផ្តើមនៅខែក្រោយ។",
		},
		Category: "lifestyle", AuthorSlug: "kim-sreyneath", MinutesAgo: 360, Views: 980,
	},
	{
		Slug: "film-festival-announces-lineup", TitleKh: "មហោស្រពភាពយន្តប្រកាសបញ្ជីរាយនាមភាពយន្ត",
		TitleEn: "Film festival announces its lineup",
		SummaryKh: "មហោស្រពឆ្នាំនេះនឹងបញ្ចាំងភាពយន្តខ្មែរ និងភាពយន្តអន្តរជាតិ។",
		Body: []string{
			"មហោស្រពភាពយន្តប្រចាំឆ្នាំបានប្រកាសបញ្ជីរាយនាមភាពយន្តដែលនឹងបញ្ចាំង។",
			"កម្មវិធីនេះរួមបញ្ចូលទាំងភាពយន្តខ្លី និងវីដេអូឯកសារពីអ្នកផលិតវ័យក្មេង។",
		},
		Category: "entertainment", AuthorSlug: "kim-sreyneath", MinutesAgo: 430, Views: 2650,
	},
	{
		Slug: "road-safety-campaign-launched", TitleKh: "យុទ្ធនាការសុវត្ថិភាពចរាចរណ៍ត្រូវបានដាក់ឱ្យដំណើរការ",
		TitleEn: "Road safety campaign launched",
		SummaryKh: "យុទ្ធនាការនេះផ្តោតលើការពាក់មួកសុវត្ថិភាព និងការបើកបរដោយប្រុងប្រយ័ត្ន។",
		Body: []string{
			"យុទ្ធនាការសុវត្ថិភាពចរាចរណ៍ថ្មីមួយបានចាប់ផ្តើម ដោយផ្តោតលើអ្នកបើកបរម៉ូតូ។",
			"អាជ្ញាធរបានលើកទឹកចិត្តឱ្យអ្នកបើកបរពាក់មួកសុវត្ថិភាពគ្រប់ពេល។",
		},
		Category: "cambodia", AuthorSlug: "sok-dara", MinutesAgo: 520, Views: 1560,
	},
	{
		Slug: "banking-sector-outlook", TitleKh: "ទស្សនវិស័យវិស័យធនាគារសម្រាប់ឆ្នាំក្រោយ",
		TitleEn: "Banking sector outlook for the year ahead",
		SummaryKh: "អ្នកវិភាគបានពិនិត្យលើនិន្នាការឥណទាន និងការប្រកួតប្រជែងក្នុងវិស័យធនាគារ។",
		Body: []string{
			"អ្នកវិភាគបានចេញផ្សាយរបាយការណ៍ស្តីពីទស្សនវិស័យវិស័យធនាគារ។",
			"របាយការណ៍នេះពិនិត្យលើនិន្នាការឥណទាន និងកម្រិតប្រាក់បញ្ញើ។",
		},
		Category: "economy", AuthorSlug: "chan-sophea", MinutesAgo: 610, Views: 870,
	},
	{
		Slug: "parliament-session-opens", TitleKh: "សម័យប្រជុំរដ្ឋសភាបើកដំណើរការ",
		TitleEn: "Parliamentary session opens",
		SummaryKh: "របៀបវារៈរួមមានការពិភាក្សាលើសេចក្តីព្រាងច្បាប់ថវិកា។",
		Body: []string{
			"សម័យប្រជុំរដ្ឋសភាបានបើកដំណើរការនៅថ្ងៃនេះ។",
			"របៀបវារៈរួមមានការពិភាក្សាលើសេចក្តីព្រាងច្បាប់ថវិកាសម្រាប់ឆ្នាំក្រោយ។",
		},
		Category: "politics", AuthorSlug: "sok-dara", MinutesAgo: 700, Views: 2140,
	},
	{
		Slug: "smart-farming-pilot-project", TitleKh: "គម្រោងសាកល្បងកសិកម្មឆ្លាតវៃនៅខេត្តបាត់ដំបង",
		TitleEn: "Smart farming pilot project in Battambang",
		SummaryKh: "កសិករកំពុងសាកល្បងឧបករណ៍វាស់សំណើមដី ដើម្បីកាត់បន្ថយការប្រើប្រាស់ទឹក។",
		Body: []string{
			"គម្រោងសាកល្បងមួយកំពុងណែនាំឧបករណ៍វាស់សំណើមដីដល់កសិករ។",
			"គោលបំណងគឺកាត់បន្ថយការប្រើប្រាស់ទឹក ខណៈរក្សាទិន្នផល។",
		},
		Category: "technology", AuthorSlug: "kim-sreyneath", MinutesAgo: 800, Views: 1120,
	},
	{
		Slug: "sponsored-savings-account-guide", TitleKh: "មគ្គុទ្ទេសក៍ជ្រើសរើសគណនីសន្សំសម្រាប់អាជីវកម្មខ្នាតតូច",
		TitleEn: "A guide to choosing a savings account for small businesses",
		SummaryKh: "អ្វីដែលគួរពិចារណានៅពេលជ្រើសរើសគណនីសន្សំសម្រាប់អាជីវកម្មរបស់អ្នក។",
		Body: []string{
			"ការជ្រើសរើសគណនីសន្សំត្រឹមត្រូវអាចជួយអាជីវកម្មខ្នាតតូចគ្រប់គ្រងលំហូរសាច់ប្រាក់។",
			"សូមពិចារណាលើថ្លៃសេវា អត្រាការប្រាក់ និងភាពងាយស្រួលក្នុងការដកប្រាក់។",
		},
		// A labelled sponsored item, so the disclosure path is visible on a
		// fresh install rather than only in tests (§42).
		Category: "business", AuthorSlug: "chan-sophea", MinutesAgo: 900, Views: 640,
		ContentType: models.ContentSponsored, Sponsor: "Demo Bank",
	},
	{
		Slug: "weekend-market-guide", TitleKh: "មគ្គុទ្ទេសក៍ផ្សារចុងសប្តាហ៍នៅរាជធានីភ្នំពេញ",
		TitleEn: "A guide to weekend markets in Phnom Penh",
		SummaryKh: "កន្លែងណាដែលអាចរកទិញផលិតផលស្រស់ និងសិប្បកម្មក្នុងស្រុក។",
		Body: []string{
			"ផ្សារចុងសប្តាហ៍នៅរាជធានីភ្នំពេញផ្តល់ជូននូវផលិតផលស្រស់ និងសិប្បកម្មក្នុងស្រុក។",
			"ផ្សារភាគច្រើនបើកពីព្រឹកព្រលឹមរហូតដល់ពេលថ្ងៃត្រង់។",
		},
		Category: "lifestyle", AuthorSlug: "kim-sreyneath", MinutesAgo: 1020, Views: 1430,
	},
}

func (s *seeder) demoArticles() (int, int, error) {
	created, existing := 0, 0
	now := time.Now().UTC()

	categories, err := s.slugToID(&models.Category{})
	if err != nil {
		return 0, 0, err
	}
	authors, err := s.slugToID(&models.Author{})
	if err != nil {
		return 0, 0, err
	}

	for _, seed := range demoArticleSeeds {
		var found models.Article
		if s.db.Where("slug = ?", seed.Slug).First(&found).Error == nil {
			existing++
			continue
		}

		categoryID, ok := categories[seed.Category]
		if !ok {
			// A demo story pointing at a category that does not exist is a bug
			// in the seed data, not something to paper over.
			return created, existing, fmt.Errorf("demo article %q references unknown category %q", seed.Slug, seed.Category)
		}

		colour := "#1E3A8A"
		for _, c := range categorySeeds {
			if c.Slug == seed.Category {
				colour = c.Color
				break
			}
		}

		body := ""
		for _, paragraph := range seed.Body {
			body += "<p>" + paragraph + "</p>"
		}

		publishedAt := now.Add(-time.Duration(seed.MinutesAgo) * time.Minute)
		wordCount := utils.CountWords(utils.StripHTML(body))

		article := models.Article{
			Slug:      seed.Slug,
			TitleKh:   seed.TitleKh,
			TitleEn:   seed.TitleEn,
			SummaryKh: seed.SummaryKh,
			ContentKh: body,
			Status:    models.StatusPublished,

			CategoryID:   categoryID,
			ImageURL:     s.articleImage(seed.Slug, iconFor(seed.Category), colour),
			ImageAltKh:   seed.TitleKh,
			ImageCaption: demoImageCaption,
			ImageWidth:   1200, ImageHeight: 675,

			IsFeatured:     seed.IsFeatured,
			PublishedAt:    &publishedAt,
			ViewCount:      seed.Views,
			UniqueViewCount: seed.Views * 7 / 10,
			WordCount:      wordCount,
			ReadingMinutes: utils.ReadingMinutes(wordCount),
		}

		if id, ok := authors[seed.AuthorSlug]; ok {
			article.AuthorID = &id
		}
		article.ContentType = seed.ContentType
		if article.ContentType == "" {
			article.ContentType = models.ContentEditorial
		}
		article.SponsorName = seed.Sponsor

		if seed.IsBreaking {
			article.IsBreaking = true
			article.BreakingStartedAt = &publishedAt
		}

		if err := s.db.Create(&article).Error; err != nil {
			return created, existing, err
		}

		// Attach a source record so the SEO audit does not flag every demo
		// article, and so the provenance workflow has an example (§19).
		source := models.ArticleSource{
			ArticleID: article.ID,
			NameKh:    "ឯកសារសាកល្បង (demo seed data)",
			Type:      models.SourceOther,
			Verification: models.VerifyUnverified,
			Notes:     "Sample content created by the seeder. Not a real source.",
		}
		if err := s.db.Create(&source).Error; err != nil {
			return created, existing, err
		}

		created++
	}

	if err := s.refreshCounts(); err != nil {
		return created, existing, err
	}
	return created, existing, nil
}

// iconFor returns the emoji used on a category's placeholder image.
func iconFor(slug string) string {
	for _, c := range categorySeeds {
		if c.Slug == slug {
			return c.Icon
		}
	}
	return "📰"
}

// refreshCounts recomputes the denormalised article counters that listing
// pages read, so the demo data is internally consistent.
func (s *seeder) refreshCounts() error {
	err := s.db.Exec(`
		UPDATE categories c SET article_count = (
			SELECT COUNT(*) FROM articles a
			WHERE a.category_id = c.id AND a.status = ? AND a.deleted_at IS NULL
		)`, models.StatusPublished).Error
	if err != nil {
		return err
	}

	return s.db.Exec(`
		UPDATE authors au SET article_count = (
			SELECT COUNT(*) FROM articles a
			WHERE a.author_id = au.id AND a.status = ? AND a.deleted_at IS NULL
		)`, models.StatusPublished).Error
}

// slugToID builds a slug → id lookup for a table that has a slug column.
func (s *seeder) slugToID(model any) (map[string]uint, error) {
	var rows []struct {
		ID   uint
		Slug string
	}
	if err := s.db.Model(model).Select("id, slug").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]uint, len(rows))
	for _, row := range rows {
		out[row.Slug] = row.ID
	}
	return out, nil
}

// ── Videos ──────────────────────────────────────────────────────────────

var demoVideoSeeds = []struct {
	Slug, TitleKh, TitleEn, Category string
	Duration, MinutesAgo             int
	Views                            int64
}{
	{"daily-briefing-evening", "ព័ត៌មានសង្ខេបប្រចាំល្ងាច", "Evening news briefing", "cambodia", 312, 60, 5400},
	{"kun-khmer-highlights", "ចំណុចសំខាន់ៗនៃការប្រកួតគុនខ្មែរ", "Kun Khmer highlights", "kun-khmer", 186, 180, 9200},
	{"market-in-60-seconds", "ទីផ្សារក្នុង ៦០ វិនាទី", "The market in 60 seconds", "business", 61, 300, 3100},
	{"street-food-short", "អាហារតាមចិញ្ចើមផ្លូវ", "Street food in 45 seconds", "lifestyle", 45, 420, 7800},
}

func (s *seeder) demoVideos() (int, int, error) {
	created, existing := 0, 0
	now := time.Now().UTC()

	categories, err := s.slugToID(&models.Category{})
	if err != nil {
		return 0, 0, err
	}

	for _, seed := range demoVideoSeeds {
		var found models.Video
		if s.db.Where("slug = ?", seed.Slug).First(&found).Error == nil {
			existing++
			continue
		}

		publishedAt := now.Add(-time.Duration(seed.MinutesAgo) * time.Minute)
		video := models.Video{
			Slug: seed.Slug, TitleKh: seed.TitleKh, TitleEn: seed.TitleEn,
			// No source file: these are catalogue entries so the video pages
			// render. The player shows a poster and no stream.
			ThumbnailURL: s.articleImage("video-"+seed.Slug, "▶", "#7C3AED"),
			ThumbnailAlt: seed.TitleKh,
			DurationSec:  seed.Duration,
			Width:        1920,
			Height:       1080,
			Status:       models.StatusPublished,
			PublishedAt:  &publishedAt,
			ViewCount:    seed.Views,
		}
		if id, ok := categories[seed.Category]; ok {
			video.CategoryID = &id
		}

		if err := s.db.Create(&video).Error; err != nil {
			return created, existing, err
		}
		created++
	}
	return created, existing, nil
}

// ── Advertising ─────────────────────────────────────────────────────────

// demoAdvertising creates one approved, running campaign so the ad slots on
// the homepage and article pages have something to serve.
func (s *seeder) demoAdvertising() (int, int, error) {
	const campaignName = "Demo house campaign"
	now := time.Now().UTC()

	var campaign models.AdvertisementCampaign
	if s.db.Where("name = ?", campaignName).First(&campaign).Error == nil {
		return 0, 1, nil
	}

	approvedAt := now
	campaign = models.AdvertisementCampaign{
		Name:       campaignName,
		Advertiser: "Cambodia Fast News (house)",
		Status:     models.AdActive,
		StartAt:    now.Add(-time.Hour),
		EndAt:      now.AddDate(1, 0, 0),
		Notes:      "Sample creatives installed by the seeder so ad slots render. Delete before selling inventory.",
		// Approved on creation because it is our own house ad. An advertiser
		// campaign would arrive unapproved and could not serve (§41).
		ApprovedAt: &approvedAt,
	}
	if err := s.db.Create(&campaign).Error; err != nil {
		return 0, 0, err
	}

	creatives := []struct {
		Name, Position          string
		DW, DH, MW, MH          int
	}{
		{"House leaderboard", models.SlotHomeTop, 970, 90, 320, 100},
		{"House sidebar", models.SlotHomeSidebar, 300, 250, 300, 250},
		{"House article top", models.SlotArticleTop, 728, 90, 320, 100},
		{"House article sidebar", models.SlotArticleSidebar, 300, 600, 300, 250},
	}

	created := 1 // the campaign itself
	for _, c := range creatives {
		ad := models.Advertisement{
			CampaignID: campaign.ID,
			Name:       c.Name,
			Position:   c.Position,
			Status:     models.AdActive,
			DesktopImageURL: placeholderImage("AD", "#172554"),
			DesktopWidth:    c.DW, DesktopHeight: c.DH,
			MobileImageURL: placeholderImage("AD", "#172554"),
			MobileWidth:    c.MW, MobileHeight: c.MH,
			TargetURL:    "/about",
			AltText:      "Sample house advertisement",
			StartAt:      now.Add(-time.Hour),
			EndAt:        now.AddDate(1, 0, 0),
			TargetDevice: "all",
			Weight:       1,
		}
		if err := s.db.Create(&ad).Error; err != nil {
			return created, 0, err
		}
		created++
	}
	return created, 0, nil
}
