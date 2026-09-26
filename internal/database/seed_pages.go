package database

import (
	"time"

	"github.com/cambodia-fast-news/backend/internal/models"
)

// pageSeeds are the standalone pages every news site is expected to have (§93).
//
// The Khmer bodies are the text that used to live in the Vue files, migrated
// verbatim so switching to database-backed pages loses nothing. BodyEn is left
// empty on purpose: an English privacy policy or set of editorial standards is a
// commitment the newsroom makes, and a machine translation would be a claim
// nobody wrote or reviewed. The public page shows a "not translated yet" notice
// until somebody fills it in.
var pageSeeds = []struct {
	Slug                   string
	Position               int
	TitleKh, TitleEn       string
	MetaDescKh, MetaDescEn string
	BodyKh                 string
}{
	{
		Slug: "privacy", Position: 1,
		TitleKh:    "គោលការណ៍ឯកជនភាព",
		TitleEn:    "Privacy Policy",
		MetaDescKh: "របៀបដែល Cambodia Fast News ប្រមូល និងប្រើប្រាស់ទិន្នន័យ។",
		MetaDescEn: "How Cambodia Fast News collects and uses data.",
		BodyKh:     "<h2>អ្វីដែលយើងប្រមូល</h2>\n<p>\n  យើងប្រមូលស្ថិតិការអានជាសរុប៖ ចំនួនទស្សនាអត្ថបទ រយៈពេលអាន ប្រភេទឧបករណ៍\n  និងប្រភពចូលមើល។ ស្ថិតិទាំងនេះត្រូវបានរក្សាទុកជាទិន្នន័យសរុបតាមថ្ងៃ\n  មិនមែនជាកំណត់ត្រាតាមបុគ្គលម្នាក់ៗទេ។\n</p>\n<p>\n  យើងមិនរក្សាទុកអាសយដ្ឋាន IP របស់អ្នកអានទេ។\n  ដើម្បីរាប់អ្នកទស្សនាម្តងគត់ យើងប្រើតម្លៃ hash ដែលមានអំបិលប្តូរជារៀងរាល់ថ្ងៃ\n  ដែលមិនអាចតាមដានអ្នកឆ្លងថ្ងៃបានឡើយ។\n</p>\n\n<h2>ការជូនដំណឹង</h2>\n<p>\n  ប្រសិនបើអ្នកជ្រើសរើសទទួលការជូនដំណឹង យើងរក្សាទុក endpoint នៃការជាវរបស់កម្មវិធីរុករក\n  និងប្រធានបទដែលអ្នកបានជ្រើសរើស។ អ្នកអាចឈប់ជាវបានគ្រប់ពេល។\n  យើងមិនផ្ញើការជូនដំណឹងសម្រាប់អត្ថបទគ្រប់ៗទេ។\n</p>\n\n<h2>ការផ្សាយពាណិជ្ជកម្ម</h2>\n<p>\n  យើងរាប់ចំនួនបង្ហាញ និងចំនួនចុចលើការផ្សាយពាណិជ្ជកម្ម ជាទិន្នន័យសរុបតាមថ្ងៃ\n  តាមទីតាំង និងតាមប្រភេទឧបករណ៍។\n</p>\n\n<h2>ព័ត៌មានដែលអ្នកផ្ញើមក</h2>\n<p>\n  ប្រសិនបើអ្នកផ្ញើព័ត៌មានមកបន្ទប់ព័ត៌មាន ព័ត៌មានទំនាក់ទំនងដែលអ្នកផ្តល់ឱ្យ\n  ត្រូវប្រើសម្រាប់ផ្ទៀងផ្ទាត់តែប៉ុណ្ណោះ ហើយមិនត្រូវបានផ្សាយទេ។\n</p>\n\n<h2>ខូគី</h2>\n<p>\n  យើងប្រើកន្លែងផ្ទុកក្នុងកម្មវិធីរុករក សម្រាប់ចំណូលចិត្តមូលដ្ឋានប៉ុណ្ណោះ\n  ដូចជាការបិទការផ្សាយពាណិជ្ជកម្មបិទភ្ជាប់។\n</p>",
	},
	{
		Slug: "terms", Position: 2,
		TitleKh:    "លក្ខខណ្ឌប្រើប្រាស់",
		TitleEn:    "Terms of Use",
		MetaDescKh: "លក្ខខណ្ឌប្រើប្រាស់គេហទំព័រ Cambodia Fast News។",
		MetaDescEn: "Terms for using the Cambodia Fast News website.",
		BodyKh:     "<h2>ការប្រើប្រាស់ខ្លឹមសារ</h2>\n<p>\n  ខ្លឹមសារទាំងអស់នៅលើគេហទំព័រនេះជាកម្មសិទ្ធិរបស់ Cambodia Fast News\n  លើកលែងតែមានការបញ្ជាក់ផ្សេង។ អ្នកអាចចែករំលែកតំណភ្ជាប់បាន។\n  ការចម្លងអត្ថបទពេញលេញដោយគ្មានការអនុញ្ញាតមិនត្រូវបានអនុញ្ញាតទេ។\n</p>\n\n<h2>ព័ត៌មានដែលអ្នកដាក់ស្នើ</h2>\n<p>\n  ដោយផ្ញើព័ត៌មាន រូបភាព ឬវីដេអូមកយើង អ្នកបញ្ជាក់ថាអ្នកមានសិទ្ធិចែករំលែកវា\n  ហើយអនុញ្ញាតឱ្យយើងប្រើវាក្នុងការរាយការណ៍ បន្ទាប់ពីការផ្ទៀងផ្ទាត់។\n</p>\n\n<h2>ភាពត្រឹមត្រូវ</h2>\n<p>\n  យើងខិតខំរាយការណ៍ឱ្យបានត្រឹមត្រូវ។ ប្រសិនបើអ្នកឃើញកំហុស សូមមើល\n  <a href=\"/correction-policy\">គោលការណ៍កែតម្រូវ</a> របស់យើង។\n</p>",
	},
	{
		Slug: "editorial-policy", Position: 3,
		TitleKh:    "គោលការណ៍វិចារណកថា",
		TitleEn:    "Editorial Policy",
		MetaDescKh: "គោលការណ៍វិចារណកថា និងស្តង់ដារសារព័ត៌មានរបស់យើង។",
		MetaDescEn: "Our editorial policy and journalistic standards.",
		BodyKh:     "<h2>ការផ្ទៀងផ្ទាត់ប្រភព</h2>\n<p>\n  រាល់អត្ថបទត្រូវមានប្រភពដែលបានកត់ត្រាទុកនៅក្នុងប្រព័ន្ធរបស់យើង រួមមានឈ្មោះប្រភព\n  ប្រភេទប្រភព ពេលវេលាទទួលបាន និងស្ថានភាពផ្ទៀងផ្ទាត់។\n  ការបង្ហោះនៅលើបណ្តាញសង្គមដែលមិនទាន់ផ្ទៀងផ្ទាត់ មិនត្រូវបានចាត់ទុកជាការពិតទេ\n  រហូតដល់អ្នកកែសម្រួលបានផ្ទៀងផ្ទាត់វាដោយផ្ទាល់។\n</p>\n\n<h2>ដំណើរការកែសម្រួល</h2>\n<p>\n  គ្មានអត្ថបទណាមួយអាចផ្សាយដោយផ្ទាល់ពីសេចក្តីព្រាងទេ។ អត្ថបទទាំងអស់ត្រូវឆ្លងកាត់\n  ការពិនិត្យដោយអ្នកកែសម្រួល មុនពេលអាចអនុម័ត និងផ្សាយ។\n  អ្នកសារព័ត៌មានមិនអាចផ្សាយអត្ថបទរបស់ខ្លួនដោយខ្លួនឯងបានទេ។\n</p>\n\n<h2>ការកែតម្រូវ</h2>\n<p>\n  នៅពេលយើងកែតម្រូវព័ត៌មានសំខាន់ណាមួយ យើងផ្សាយសេចក្តីជូនដំណឹងកែតម្រូវនៅលើអត្ថបទនោះ\n  ដោយបញ្ជាក់ពីអ្វីដែលបានផ្លាស់ប្តូរ និងពេលវេលា។\n  កំណែមុនត្រូវបានរក្សាទុកនៅក្នុងប្រវត្តិកំណែផ្ទៃក្នុង។\n  យើងមិនផ្លាស់ប្តូរព័ត៌មានពិតដោយស្ងៀមស្ងាត់ឡើយ។\n</p>\n\n<h2>ការប្រើប្រាស់ AI</h2>\n<p>\n  យើងប្រើឧបករណ៍ AI ដើម្បីជួយរៀបចំសេចក្តីព្រាង សង្ខេប និងទិន្នន័យ SEO។\n  AI មិនដែលផ្សាយអ្វីមួយដោយស្វ័យប្រវត្តិទេ។\n  រាល់លទ្ធផលពី AI ត្រូវបានពិនិត្យដោយអ្នកកែសម្រួលជាមនុស្សមុនពេលផ្សាយ។\n</p>\n<p>\n  AI មិនត្រូវបានអនុញ្ញាតឱ្យបង្កើតសម្រង់ ស្ថិតិ ឈ្មោះមនុស្ស ព្រឹត្តិការណ៍ កាលបរិច្ឆេទ\n  ទីកន្លែង ឬប្រភពឡើយ។ វាធ្វើការតែលើព័ត៌មានដែលបានផ្ទៀងផ្ទាត់ដែលអ្នកសារព័ត៌មានផ្តល់ឱ្យ។\n  សង្ខេបដែលបង្កើតដោយ AI ត្រូវបានដាក់ស្លាកច្បាស់លាស់ និងបង្ហាញដាច់ដោយឡែកពីអត្ថបទដើម។\n</p>\n\n<h2>រូបភាពបង្កើតដោយ AI</h2>\n<p>\n  រូបភាពដែលបង្កើតដោយ AI ត្រូវបានដាក់ស្លាកជារូបភាពឧទាហរណ៍។\n  យើងមិនបង្ហាញរូបភាព AI ជារូបថតពិតនៃព្រឹត្តិការណ៍ណាមួយឡើយ។\n</p>\n\n<h2>ខ្លឹមសារឧបត្ថម្ភ</h2>\n<p>\n  ខ្លឹមសារដែលបានបង់ប្រាក់ត្រូវបានដាក់ស្លាកច្បាស់លាស់ថាជាខ្លឹមសារឧបត្ថម្ភ\n  ទាំងនៅក្នុងបញ្ជីព័ត៌មាន និងនៅលើទំព័រអត្ថបទ រួមទាំងឈ្មោះអ្នកឧបត្ថម្ភ។\n  ខ្លឹមសារឧបត្ថម្ភមិនត្រូវបានបង្ហាញជាការរាយការណ៍ឯករាជ្យឡើយ។\n</p>\n\n<h2>ព័ត៌មានពីអ្នកអាន</h2>\n<p>\n  ព័ត៌មានដែលអ្នកអានផ្ញើមកត្រូវបានពិនិត្យដោយអ្នកកែសម្រួល។\n  គ្មានការដាក់ស្នើណាមួយត្រូវបានផ្សាយដោយស្វ័យប្រវត្តិទេ។\n</p>\n\n<h2>ឯករាជ្យភាពវិចារណកថា</h2>\n<p>\n  ការសម្រេចចិត្តវិចារណកថាត្រូវបានធ្វើឡើងដោយបន្ទប់ព័ត៌មាន។\n  អ្នកផ្សាយពាណិជ្ជកម្មមិនមានសិទ្ធិលើខ្លឹមសារវិចារណកថាឡើយ។\n</p>",
	},
	{
		Slug: "correction-policy", Position: 4,
		TitleKh:    "គោលការណ៍កែតម្រូវ",
		TitleEn:    "Correction Policy",
		MetaDescKh: "របៀបដែលយើងកែតម្រូវកំហុស។",
		MetaDescEn: "How we correct mistakes.",
		BodyKh:     "<h2>របៀបដែលយើងកែតម្រូវ</h2>\n<p>\n  នៅពេលយើងដឹងថាមានកំហុស យើងកែតម្រូវវាឱ្យបានឆាប់តាមដែលអាចធ្វើទៅបាន។\n  សម្រាប់កំហុសពិតដែលសំខាន់ យើងបន្ថែមសេចក្តីជូនដំណឹងកែតម្រូវនៅលើអត្ថបទ\n  ដែលបញ្ជាក់ពីអ្វីដែលបានផ្លាស់ប្តូរ និងពេលវេលាកែតម្រូវ។\n</p>\n\n<h2>អ្វីដែលយើងរក្សាទុក</h2>\n<p>\n  រាល់ការផ្លាស់ប្តូរលើអត្ថបទដែលបានផ្សាយរួច ត្រូវបានរក្សាទុកជាកំណែផ្ទៃក្នុង\n  រួមទាំងឈ្មោះអ្នកកែសម្រួល ពេលវេលា និងហេតុផល។\n</p>\n\n<h2>រាយការណ៍កំហុស</h2>\n<p>\n  ប្រសិនបើអ្នកឃើញកំហុសក្នុងអត្ថបទណាមួយ សូម\n  <a href=\"/contact\">ទំនាក់ទំនងមកយើង</a> ដោយបញ្ជាក់តំណអត្ថបទ\n  និងអ្វីដែលអ្នកគិតថាមិនត្រឹមត្រូវ។\n</p>",
	},
	{
		Slug: "about", Position: 5,
		TitleKh:    "អំពីយើង",
		TitleEn:    "About",
		MetaDescKh: "អំពី Cambodia Fast News។",
		MetaDescEn: "About Cambodia Fast News.",
		BodyKh:     "<p>\n  Cambodia Fast News (ព័ត៌មានលឿនរហ័សកម្ពុជា) គឺជាបន្ទប់ព័ត៌មានឌីជីថល\n  ដែលផ្តោតលើព័ត៌មានទាន់ហេតុការណ៍សម្រាប់អ្នកអានកម្ពុជា។\n</p>\n<p>\n  យើងផ្តល់អាទិភាពដល់ភាពរហ័ស ភាពត្រឹមត្រូវ និងភាពងាយស្រួលអាននៅលើទូរស័ព្ទ។\n</p>\n\n<h2>គោលការណ៍របស់យើង</h2>\n<ul>\n  <li>ផ្ទៀងផ្ទាត់មុនផ្សាយ</li>\n  <li>កែតម្រូវដោយបើកចំហ</li>\n  <li>ដាក់ស្លាកខ្លឹមសារឧបត្ថម្ភឱ្យច្បាស់លាស់</li>\n  <li>ប្រើ AI ជាឧបករណ៍ជំនួយ មិនមែនជាអ្នកផ្សាយ</li>\n</ul>\n\n<p>\n  សូមអាន <a href=\"/editorial-policy\">គោលការណ៍វិចារណកថា</a> របស់យើងសម្រាប់ព័ត៌មានលម្អិត។\n</p>",
	},
	{
		Slug: "contact", Position: 6,
		TitleKh:    "ទំនាក់ទំនង",
		TitleEn:    "Contact",
		MetaDescKh: "ទំនាក់ទំនងបន្ទប់ព័ត៌មាន Cambodia Fast News។",
		MetaDescEn: "Contact the Cambodia Fast News newsroom.",
		BodyKh:     "<p>\n  សម្រាប់ព័ត៌មាន សំណួរវិចារណកថា ឬការរាយការណ៍កំហុស សូមទាក់ទងមកបន្ទប់ព័ត៌មាន។\n</p>\n\n<h2>ផ្ញើព័ត៌មាន</h2>\n<p>\n  ប្រសិនបើអ្នកមានព័ត៌មានសម្រាប់យើង សូមប្រើ\n  <a href=\"/tip\">ទម្រង់ផ្ញើព័ត៌មាន</a>។\n</p>\n\n<h2>ការផ្សាយពាណិជ្ជកម្ម</h2>\n<p>\n  សម្រាប់ការសាកសួរអំពីការផ្សាយពាណិជ្ជកម្ម សូមទាក់ទងផ្នែកពាណិជ្ជកម្ម។\n</p>\n\n<!-- Contact details are intentionally left for the newsroom to fill in\n     rather than invented here. -->\n<p class=\"text-ink-muted\">\n  <em>ព័ត៌មានទំនាក់ទំនងនឹងត្រូវបន្ថែមនៅពេលដាក់ឱ្យដំណើរការ។</em>\n</p>",
	},
}

// pages installs the standalone pages. Existing rows are left untouched: once an
// operator has edited the privacy policy, a later deploy must not overwrite it.
func (s *seeder) pages() (int, int, error) {
	created, existing := 0, 0
	now := time.Now().UTC()

	for _, seed := range pageSeeds {
		if restoreSoftDeleted(s.db, &models.Page{}, seed.Slug) {
			existing++
			continue
		}
		var found models.Page
		if s.db.Where("slug = ?", seed.Slug).First(&found).Error == nil {
			existing++
			continue
		}

		page := models.Page{
			Slug: seed.Slug, Position: seed.Position,
			TitleKh: seed.TitleKh, TitleEn: seed.TitleEn,
			MetaDescKh: seed.MetaDescKh, MetaDescEn: seed.MetaDescEn,
			BodyKh: seed.BodyKh,
			// Flags are set explicitly rather than relying on column defaults:
			// GORM writes the default in place of a Go false.
			IsPublished:  true,
			ShowInFooter: true,
			IsSystem:     true,
			PublishedAt:  &now,
		}
		if err := s.db.Create(&page).Error; err != nil {
			return created, existing, err
		}
		created++
	}
	return created, existing, nil
}
