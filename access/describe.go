package access

import "context"

// LimitationView describes a limitation type for the role editor. Choices is nil for free text.
type LimitationView struct {
	Name     string   `json:"name"`
	Choices  []Choice `json:"choices"`
	Policies []string `json:"policies"`
}

// CatalogView is the policies roles can grant and the limitations that narrow them.
type CatalogView struct {
	Policies    []Policy         `json:"policies"`
	Limitations []LimitationView `json:"limitations"`
}

// Describe lists the catalog with each limitation's choices and the policies it applies to.
func (c *Catalog) Describe(ctx context.Context) (CatalogView, error) {
	v := CatalogView{Policies: c.Policies(), Limitations: []LimitationView{}}
	for _, l := range c.Limitations() {
		choices, err := l.Choices(ctx)
		if err != nil {
			return CatalogView{}, err
		}
		lv := LimitationView{Name: l.Name(), Choices: choices, Policies: []string{}}
		for _, p := range v.Policies {
			for _, n := range p.Limitations {
				if n == lv.Name {
					lv.Policies = append(lv.Policies, p.Name)
				}
			}
		}
		v.Limitations = append(v.Limitations, lv)
	}
	for i := range v.Policies {
		if v.Policies[i].Limitations == nil {
			v.Policies[i].Limitations = []string{}
		}
	}
	return v, nil
}
