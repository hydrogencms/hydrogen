package main

// homeFields is the home page; about is the id of the About page.
func homeFields(about string) map[string]any {
	return map[string]any{
		"title":       "Home",
		"description": "A static website with its content in git.",
		"body": []any{
			hero("Welcome to your new site",
				"Edit this page in the admin, save the content, commit it and push: GitHub Pages publishes the result.",
				"About this site", about),
			features("What you get",
				feature("Content in git", "The content lives in the content directory of the repository, so every change is reviewed and versioned like code."),
				feature("A static site", "Astro builds plain HTML from the published content. There is no server to run or pay for."),
				feature("An editor for everyone", "Editors work in the admin with a live preview and never touch the repository.")),
			callout("This is starter content. Change it in the admin, or replace it with your own.", "tip"),
		},
	}
}

// aboutFields is the About page; home is the id of the home page.
func aboutFields(home string) map[string]any {
	return map[string]any{
		"title":       "About",
		"description": "How this site is made.",
		"body": []any{
			text("How this site is made",
				para(txt("The pages of this site were written in the admin and saved to the content directory. Start at the "), link("home page", home), txt(".")),
				para(txt("A build starts the CMS on that directory and renders every page to a file.")),
				bullets("Add pages and blocks in the admin.", "Run hydrogen content:save, then commit and push.", "The workflow in .github/workflows builds and deploys the site.")),
			callout("Add a block type in schema/blocks.yaml and a component in site/src/components/blocks.", "note"),
		},
	}
}
