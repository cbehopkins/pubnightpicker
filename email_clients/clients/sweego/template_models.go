package sweego

type CreateTemplateRequest struct {
	Name     string `json:"name"`
	Template string `json:"template"`
}

type UpdateTemplateRequest struct {
	Name         string `json:"name"`
	Template     string `json:"template"`
	TemplateType string `json:"template_type"`
}
