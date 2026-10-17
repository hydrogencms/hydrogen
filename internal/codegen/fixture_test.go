package codegen

import "github.com/hydrogencms/hydrogen/schema"

func str(name, format string, required bool) *schema.Field {
	return &schema.Field{Name: name, Type: "string", Required: required, Shape: schema.Shape{Base: schema.BaseString, Format: format}}
}

func fixture() ([]*schema.Type, []*schema.Group) {
	seo := &schema.Group{
		Name:        "seo",
		Description: "Search engine metadata shared by pages.",
		Fields: []*schema.Field{
			str("metaTitle", "", false),
			{Name: "noIndex", Type: "boolean", Shape: schema.Shape{Base: schema.BaseBoolean}},
		},
	}
	article := &schema.Type{
		Name:        "article",
		Description: "A news or video article published on the site.",
		Fields: []*schema.Field{
			{Name: "title", Type: "string", Required: true, Description: "The headline shown on the page and in listings.", Shape: schema.Shape{Base: schema.BaseString}},
			str("slug", "slug", true),
			str("summary", "text", false),
			str("contactEmail", "email", false),
			str("sourceUrl", "url", false),
			{Name: "kind", Type: "enum", Required: true, Shape: schema.Shape{Base: schema.BaseString, Format: "enum", Enum: []string{"news", "video", "long-read", "2fa"}}},
			{Name: "rating", Type: "number", Shape: schema.Shape{Base: schema.BaseNumber}},
			{Name: "views", Type: "number", Required: true, Shape: schema.Shape{Base: schema.BaseNumber, Integer: true}},
			{Name: "pinned", Type: "boolean", Required: true, Shape: schema.Shape{Base: schema.BaseBoolean}},
			{Name: "publishDate", Type: "date", Description: "Day the article goes live. Shown in the site's time zone, so keep it a plain calendar date rather than a timestamp.", Shape: schema.Shape{Base: schema.BaseString, Format: "date"}},
			{Name: "embargo", Type: "datetime", Shape: schema.Shape{Base: schema.BaseString, Format: "datetime"}},
			{Name: "author", Type: "ref", Required: true, Shape: schema.Shape{Base: schema.BaseRef, To: []string{"person"}}},
			{Name: "body", Type: "richtext", Required: true, Shape: schema.Shape{Base: schema.BaseRichText}},
			{Name: "teaser", Type: "richtext", Shape: schema.Shape{Base: schema.BaseRichText}},
			{Name: "extra", Type: "json", Shape: schema.Shape{Base: schema.BaseJSON}},
			{Name: "tags", Type: "list", Shape: schema.Shape{Base: schema.BaseList}, Of: &schema.Field{Type: "string", Shape: schema.Shape{Base: schema.BaseString}}},
			{Name: "related", Type: "list", Shape: schema.Shape{Base: schema.BaseList}, Of: &schema.Field{Type: "ref", Shape: schema.Shape{Base: schema.BaseRef, To: []string{"article"}}}},
			{Name: "channels", Type: "list", Shape: schema.Shape{Base: schema.BaseList}, Of: &schema.Field{Type: "enum", Shape: schema.Shape{Base: schema.BaseString, Format: "enum", Enum: []string{"web", "app"}}}},
			{Name: "address", Type: "group", Shape: schema.Shape{Base: schema.BaseGroup}, Fields: []*schema.Field{
				str("city", "", true),
				{Name: "geo", Type: "group", Shape: schema.Shape{Base: schema.BaseGroup}, Fields: []*schema.Field{
					{Name: "lat", Type: "number", Required: true, Shape: schema.Shape{Base: schema.BaseNumber}},
					{Name: "lng", Type: "number", Required: true, Shape: schema.Shape{Base: schema.BaseNumber}},
				}},
			}},
			{Name: "venue", Type: "group", Required: true, Shape: schema.Shape{Base: schema.BaseGroup}, Fields: []*schema.Field{str("room", "", false)}},
			{Name: "links", Type: "list", Shape: schema.Shape{Base: schema.BaseList}, Of: &schema.Field{Type: "group", Shape: schema.Shape{Base: schema.BaseGroup}, Fields: []*schema.Field{
				str("label", "", true), str("href", "url", true),
			}}},
			{Name: "seo", Type: "group", Group: "seo", Shape: schema.Shape{Base: schema.BaseGroup}},
			{Name: "legal", Type: "group", Group: "seo", Required: true, Shape: schema.Shape{Base: schema.BaseGroup}},
			{Name: "videoUrl", Type: "url", When: &schema.Condition{Field: "kind", Op: "eq", Value: "video"}, Required: true, Shape: schema.Shape{Base: schema.BaseString, Format: "url"}},
			{Name: "userId", Type: "string", Shape: schema.Shape{Base: schema.BaseString}},
			{Name: "hero", Type: "asset", Shape: schema.Shape{Base: schema.BaseRef, Format: "asset", To: []string{"image"}}},
			{Name: "gallery", Type: "list", Shape: schema.Shape{Base: schema.BaseList}, Of: &schema.Field{Type: "asset", Shape: schema.Shape{Base: schema.BaseRef, Format: "asset", To: []string{"image"}}}},
			{Name: "attachment", Type: "file", Shape: schema.Shape{Base: schema.BaseFile}},
			{Name: "html_body", Type: "text", Shape: schema.Shape{Base: schema.BaseString, Format: "text"}},
		},
	}
	person := &schema.Type{
		Name: "person",
		Fields: []*schema.Field{
			str("fullName", "", true),
		},
	}
	image := &schema.Type{
		Name:   "image",
		Upload: &schema.Upload{Accept: []string{"image/*"}},
		Fields: []*schema.Field{
			{Name: "file", Type: "file", Required: true, Shape: schema.Shape{Base: schema.BaseFile}},
			{Name: "folder", Type: "string", Shape: schema.Shape{Base: schema.BaseString}},
			{Name: "tags", Type: "list", Shape: schema.Shape{Base: schema.BaseList}, Of: &schema.Field{Type: "string", Shape: schema.Shape{Base: schema.BaseString}}},
			str("alt", "", false),
		},
	}
	home := &schema.Type{
		Name:      "blogPost",
		Singleton: true,
		Fields: []*schema.Field{
			{Name: "seo", Type: "group", Group: "seo", Shape: schema.Shape{Base: schema.BaseGroup}},
		},
	}
	return []*schema.Type{person, home, article, image}, []*schema.Group{seo}
}
