package models

// Task is the simple task structure for WebSocket communication
type Task struct {
	Type     string `json:"type"`
	SiteKey  string `json:"sitekey"`
	URL      string `json:"url"`
	TaskId   int64  `json:"task_id"`
	Solution string `json:"solution,omitempty"` // For sending solutions back
}

// CaptchaTask описывает задачу по решению капчи
type CaptchaTask struct {
	ID              int64  `json:"id"`
	UserID          int64  `json:"user_id"`             // пользователь, отправивший задачу
	SolverID        int64  `json:"solver_id,omitempty"` // пользователь, решивший задачу (если есть)
	CaptchaType     string `json:"captcha_type"`
	SiteKey         string `json:"sitekey"`
	TargetURL       string `json:"target_url"`
	CaptchaResponse string `json:"captcha_response,omitempty"`
}
