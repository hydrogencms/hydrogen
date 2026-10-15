package main

type entry struct {
	name  string
	typ   string
	title map[string]string
	draft bool // never published
	// fields are the translatable fields of one locale; nil: the item has no such locale.
	fields func(loc string, r refs) map[string]any
}

func pick(loc, en, de string) string {
	if loc == "de" {
		return de
	}
	return en
}

func body(title string, blocks ...any) map[string]any {
	return map[string]any{"title": title, "body": blocks}
}

var settings = map[string]map[string]any{
	"en": {"siteName": "Starter", "footerText": "Built with Hydrogen. Content is managed in the admin and rendered by your frontend."},
	"de": {"siteName": "Starter", "footerText": "Mit Hydrogen gebaut. Inhalte werden im Admin gepflegt und vom Frontend dargestellt."},
}

// catalog lists the items in creation order (ids follow it).
var catalog = []entry{
	{name: "getInTouch", typ: "sharedBlock", title: map[string]string{"en": "Get in touch"},
		fields: func(loc string, r refs) map[string]any {
			return map[string]any{"title": pick(loc, "Get in touch", "Kontakt aufnehmen"), "content": []any{
				callout(pick(loc, "Questions? We are happy to help.", "Fragen? Wir helfen gern."), "brand"),
				richText(para(txt(pick(loc, "Reach the team on the ", "Das Team erreichen Sie auf der ")),
					link(pick(loc, "contact page", "Kontaktseite"), r.id("contact")), txt("."))),
			}}
		}},

	{name: "home", typ: "landing", title: map[string]string{"en": "Home", "de": "Startseite"},
		fields: func(loc string, r refs) map[string]any {
			return body(pick(loc, "Home", "Startseite"),
				hero(pick(loc, "Build with blocks", "Mit Bausteinen bauen"),
					pick(loc, "A starter site: pages made of blocks, rendered by your frontend.", "Eine Starter-Seite: Seiten aus Bausteinen, dargestellt von Ihrem Frontend."),
					"dark", pick(loc, "About us", "Über uns"), r.id("about")),
				cards(pick(loc, "What we do", "Was wir tun"), "3",
					card(pick(loc, "Services", "Leistungen"), pick(loc, "What we offer, from strategy to delivery.", "Was wir anbieten, von der Strategie bis zur Umsetzung."), r.id("services")),
					card(pick(loc, "Team", "Team"), pick(loc, "The people behind the work.", "Die Menschen hinter der Arbeit."), r.id("team")),
					card(pick(loc, "Careers", "Karriere"), pick(loc, "Join us and build something good.", "Arbeiten Sie mit uns."), r.id("careers"))),
				richText(heading(2, pick(loc, "Pages that stay flexible", "Seiten, die flexibel bleiben")),
					para(txt(pick(loc, "Editors arrange blocks; developers decide how each block looks. Read the ", "Redaktion ordnet Bausteine an, Entwickler bestimmen ihr Aussehen. Lesen Sie den ")),
						link(pick(loc, "latest post", "neuesten Beitrag"), r.id("blocksPost")), txt(".")),
					bullets(pick(loc, "Typed blocks and design tokens", "Typisierte Bausteine und Design-Tokens"),
						pick(loc, "Live preview while you edit", "Live-Vorschau beim Bearbeiten"))),
				image(pic("home"), pick(loc, "A workspace with a laptop", "Ein Arbeitsplatz mit Laptop"), pick(loc, "Placeholder image", "Platzhalterbild")),
				callout(pick(loc, "This is a fixture: replace it with your own content.", "Dies ist ein Beispiel: Ersetzen Sie es durch Ihre Inhalte."), "light"),
				shared(r.id("getInTouch")))
		}},

	{name: "about", typ: "page", title: map[string]string{"en": "About", "de": "Über uns"},
		fields: func(loc string, r refs) map[string]any {
			return body(pick(loc, "About", "Über uns"),
				richText(heading(2, pick(loc, "Who we are", "Wer wir sind")),
					para(txt(pick(loc, "We are a small team that likes ", "Wir sind ein kleines Team, das ")), bold(pick(loc, "simple, durable", "einfache, langlebige")),
						txt(pick(loc, " software. Meet the ", " Software mag. Lernen Sie das ")), link("Team", r.id("team")), txt(".")),
					para(txt(pick(loc, "Looking for work? See ", "Auf Jobsuche? Sehen Sie ")), link(pick(loc, "open roles", "offene Stellen"), r.id("careers")), txt("."))),
				image(pic("about"), pick(loc, "Our office", "Unser Büro"), ""),
				shared(r.id("getInTouch")))
		}},

	{name: "services", typ: "page", title: map[string]string{"en": "Services", "de": "Leistungen"},
		fields: func(loc string, r refs) map[string]any {
			return body(pick(loc, "Services", "Leistungen"),
				richText(para(txt(pick(loc, "Four ways we can help.", "Vier Wege, wie wir helfen können.")))),
				cards("", "4",
					card(pick(loc, "Strategy", "Strategie"), pick(loc, "Find the right problem.", "Das richtige Problem finden."), ""),
					card(pick(loc, "Design", "Gestaltung"), pick(loc, "Make it clear and kind.", "Klar und freundlich gestalten."), ""),
					card(pick(loc, "Build", "Umsetzung"), pick(loc, "Ship it, keep it small.", "Ausliefern und klein halten."), ""),
					card(pick(loc, "Support", "Betreuung"), pick(loc, "Stay with you afterwards.", "Auch danach an Ihrer Seite."), r.id("contact"))),
				image(pic("services"), pick(loc, "Sketches on a table", "Skizzen auf einem Tisch"), pick(loc, "From sketch to site", "Von der Skizze zur Seite")))
		}},

	{name: "contact", typ: "page", title: map[string]string{"en": "Contact", "de": "Kontakt"},
		fields: func(loc string, r refs) map[string]any {
			return body(pick(loc, "Contact", "Kontakt"),
				richText(para(txt(pick(loc, "Write to hello@example.com or call us on weekdays.", "Schreiben Sie an hello@example.com oder rufen Sie uns werktags an.")))),
				callout(pick(loc, "We answer within two working days.", "Wir antworten innerhalb von zwei Werktagen."), "brand"))
		}},

	{name: "soon", typ: "landing", title: map[string]string{"en": "Coming soon"}, draft: true,
		fields: func(loc string, r refs) map[string]any {
			if loc != "en" {
				return nil
			}
			return body("Coming soon", hero("Coming soon", "A new page is on its way.", "brand", "", ""))
		}},

	{name: "team", typ: "page", title: map[string]string{"en": "Team", "de": "Team"},
		fields: func(loc string, r refs) map[string]any {
			return body("Team",
				richText(para(txt(pick(loc, "A few of us.", "Einige von uns.")))),
				cards("", "2",
					card("Ada", pick(loc, "Engineering", "Entwicklung"), ""),
					card("Grace", pick(loc, "Design", "Gestaltung"), "")))
		}},

	{name: "careers", typ: "page", title: map[string]string{"en": "Careers", "de": "Karriere"},
		fields: func(loc string, r refs) map[string]any {
			return body(pick(loc, "Careers", "Karriere"),
				richText(para(txt(pick(loc, "We hire slowly and on purpose.", "Wir stellen langsam und mit Bedacht ein.")))),
				callout(pick(loc, "No open roles right now.", "Zurzeit keine offenen Stellen."), "light"))
		}},

	{name: "helloPost", typ: "post", title: map[string]string{"en": "Hello, world", "de": "Hallo Welt"},
		fields: func(loc string, r refs) map[string]any {
			return map[string]any{"title": pick(loc, "Hello, world", "Hallo Welt"),
				"excerpt": pick(loc, "Our first post.", "Unser erster Beitrag."),
				"body": []any{
					richText(para(txt(pick(loc, "Welcome to the blog.", "Willkommen im Blog.")))),
					image(pic("hello"), pick(loc, "A sunrise", "Ein Sonnenaufgang"), "")}}
		}},

	{name: "releasePost", typ: "post", title: map[string]string{"en": "Release notes", "de": "Neuerungen"},
		fields: func(loc string, r refs) map[string]any {
			return map[string]any{"title": pick(loc, "Release notes", "Neuerungen"),
				"excerpt": pick(loc, "What changed this month.", "Was sich diesen Monat geändert hat."),
				"body": []any{
					callout(pick(loc, "Version 1.1 is out.", "Version 1.1 ist erschienen."), "brand"),
					richText(bullets(pick(loc, "Faster pages", "Schnellere Seiten"), pick(loc, "New blocks", "Neue Bausteine")))}}
		}},

	{name: "blocksPost", typ: "post", title: map[string]string{"en": "Building with blocks", "de": "Mit Bausteinen bauen"},
		fields: func(loc string, r refs) map[string]any {
			return map[string]any{"title": pick(loc, "Building with blocks", "Mit Bausteinen bauen"),
				"excerpt": pick(loc, "How block-based pages work.", "Wie blockbasierte Seiten funktionieren."),
				"body": []any{
					richText(heading(2, pick(loc, "Blocks", "Bausteine")),
						para(txt(pick(loc, "Every page is a list of blocks. See also the ", "Jede Seite ist eine Liste von Bausteinen. Siehe auch die ")),
							link(pick(loc, "services", "Leistungen"), r.id("services")), txt("."))),
					shared(r.id("getInTouch"))}}
		}},
}
