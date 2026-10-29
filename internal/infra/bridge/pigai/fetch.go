package pigai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"

	"catcher/internal/domain/entity"
)

const (
	// unknownClassID 是没有班级归属时的兜底课程
	unknownClassID   = "pigai:class:unknown"
	unknownClassName = "批改网未分类"
	// classKeyPrefix 是班级名匹配不上时造的课程前缀
	classKeyPrefix = "pigai:class:"
)

// mergedItem 是两个列表合并后的一条待写作业
type mergedItem struct {
	requestID   string
	status      entity.SubmissionStatus
	title       string
	essayID     string
	stuClass    string
	deadlineRaw string
	createRaw   string
	detailURL   string
}

// fetchRun 保存一次抓取过程中的课程登记表与告警
type fetchRun struct {
	result  *entity.FetchResult
	courses map[string]*courseRef
	byName  map[string]*courseRef
	order   []*courseRef
	now     *time.Time
}

type courseRef struct {
	id   string
	name string
}

// Fetch 抓取批改网的班级、未完成作业与已完成作文。
//
// 分页参数未证实，只抓第一页，所以 Complete 与 CourseListComplete 恒为 false：
// 宁可每次刷新都是 partial，也不能让不完整的列表触发软删除
func (c *Client) Fetch(ctx context.Context, account entity.Account, session entity.Session,
	opts entity.FetchOptions, progress entity.ProgressFunc) (*entity.FetchResult, error) {
	_ = account
	secret, err := decodeSecret(session.Secret)
	if err != nil {
		return nil, err
	}
	if progress == nil {
		progress = func(entity.Progress) {}
	}
	now := c.now()
	if opts.Now != nil {
		now = opts.Now()
	}

	jar := newCookieJar(secret.Cookies)
	run := &fetchRun{
		result:  &entity.FetchResult{},
		courses: make(map[string]*courseRef),
		byName:  make(map[string]*courseRef),
	}

	// 未完成列表同时带出班级表，班级表只在这一个响应里出现
	progress(entity.Progress{Platform: entity.PlatformPigai, Phase: "courses", Message: "读取班级与未完成作业"})
	pending, err := c.fetchPending(ctx, jar)
	if err != nil {
		// 主数据源失败就整次失败，不能返回空列表冒充「没有作业」
		return nil, err
	}
	if err := c.persistCookies(jar, opts); err != nil {
		return nil, fmt.Errorf("保存批改网 cookie 失败: %w", err)
	}
	run.registerClasses(pending.StduntClass)
	run.observeServerNow(pending.NowTime)

	progress(entity.Progress{Platform: entity.PlatformPigai, Phase: "completed", Message: "读取已完成作文"})
	completed, err := c.fetchCompleted(ctx, jar)
	if err != nil {
		if errors.Is(err, entity.ErrAuthExpired) || errors.Is(err, entity.ErrCanceled) {
			return nil, err
		}
		// 已完成列表失败不影响未完成部分，降级成警告
		run.warn(entity.ScopePlatform, "", issueKindOf(err),
			fmt.Sprintf("已完成作文列表读取失败，本次只写入未完成作业: %v", err))
		completed = nil
	}

	var essays []essayItem
	if completed != nil {
		essays = completed.EssayList
	}
	items := c.mergeItems(pending.RequestInfo, essays)
	start := entity.SemesterStart(now)

	for i, item := range items {
		progress(entity.Progress{
			Platform: entity.PlatformPigai,
			Phase:    "details",
			Current:  i + 1,
			Total:    len(items),
			Message:  item.title,
		})

		// 已完成列表的标题只是「第N号 作文」，只有本学期的条目才值得再调一次详情
		wantDetail := item.status == entity.StatusUnsubmitted || run.isCurrentTerm(item, start)
		var detail *detailData
		if wantDetail {
			detail, err = c.fetchDetail(ctx, item.detailURL, jar)
			if err != nil {
				if errors.Is(err, entity.ErrAuthExpired) || errors.Is(err, entity.ErrCanceled) {
					return nil, err
				}
				// 单条详情失败不拖垮整次刷新，作业保留并退到兜底课程
				run.warn(entity.ScopeAssignment, entity.PigaiDedupKey(item.requestID), issueKindOf(err),
					fmt.Sprintf("作业 %q 详情抓取失败: %v", item.title, err))
				detail = nil
			}
		}

		result := run.toAssignment(item, detail)
		run.result.Assignments = append(run.result.Assignments, result)
	}

	if err := c.persistCookies(jar, opts); err != nil {
		return nil, fmt.Errorf("保存批改网 cookie 失败: %w", err)
	}

	for _, ref := range run.order {
		run.result.Courses = append(run.result.Courses, entity.Course{
			Platform:   entity.PlatformPigai,
			ExternalID: ref.id,
			Name:       ref.name,
			// 平台没有学期字段；从班级名硬推学期没有收益，留空
			SemesterKey: "",
		})
	}
	run.result.ServerNow = run.now
	run.result.Complete = false
	run.result.CourseListComplete = false

	progress(entity.Progress{
		Platform: entity.PlatformPigai,
		Phase:    "done",
		Current:  len(items),
		Total:    len(items),
		Message:  "抓取完成",
	})
	return run.result, nil
}

// toAssignment 把一条合并结果和它的详情折成实体
func (r *fetchRun) toAssignment(item *mergedItem, detail *detailData) entity.Assignment {
	title := strings.TrimSpace(item.title)
	deadlineRaw := item.deadlineRaw
	deadline := r.parseDeadline(item.deadlineRaw, entity.PigaiDedupKey(item.requestID))
	description := ""

	var course *courseRef
	switch item.status {
	case entity.StatusUnsubmitted:
		if detail != nil {
			if t := strings.TrimSpace(detail.Request.EssayTitle); t != "" {
				title = t
			}
			description = stripHTML(detail.Request.EssayTopic)
			if len(detail.TClass) > 0 {
				course = r.courseByID(detail.TClass[0].ClassID, detail.TClass[0].Class)
			}
		}
		if course == nil {
			if item.stuClass != "" {
				course = r.courseByName(item.stuClass)
			} else {
				course = r.unknownCourse()
			}
		}
	default:
		if detail != nil {
			if t := strings.TrimSpace(detail.Request.EssayTitle); t != "" {
				title = t
			}
			if d := r.parseDeadline(detail.Request.ValidEndTime, entity.PigaiDedupKey(item.requestID)); d != nil {
				deadline = d
				deadlineRaw = detail.Request.ValidEndTime
			}
			description = stripHTML(detail.Request.EssayTopic)
		}
		className := strings.TrimSpace(item.stuClass)
		if className == "" && detail != nil {
			className = strings.TrimSpace(detail.Essay.StuClass)
		}
		course = r.courseByName(className)
	}
	if detail != nil {
		r.observeServerNow(detail.NowTime)
	}
	if title == "" {
		title = "第" + item.requestID + "号 作文"
	}

	source := entity.DeadlineUnknown
	if deadline != nil {
		source = entity.DeadlinePlatformExact
	}

	return entity.Assignment{
		Platform:         entity.PlatformPigai,
		CourseExternalID: course.id,
		CourseName:       course.name,
		ExternalID:       item.requestID,
		DedupKey:         entity.PigaiDedupKey(item.requestID),
		Title:            title,
		Status:           item.status,
		Deadline:         deadline,
		DeadlineSource:   source,
		DeadlineRaw:      deadlineRaw,
		DetailURL:        item.detailURL,
		Description:      description,
	}
}

// parseDeadline 解析截止时间。解析失败只记告警并按未知处理，不猜测
func (r *fetchRun) parseDeadline(raw, ref string) *time.Time {
	t, err := parseUnixSeconds(raw)
	if err != nil {
		r.warn(entity.ScopeAssignment, ref, entity.IssueParse, "截止时间无法解析: "+err.Error())
		return nil
	}
	return t
}

// isCurrentTerm 判断一条已完成作文是否属于本学期。
// 平台没有学期字段，按 9 月 1 日划线；时间拿不到的按本学期处理
func (r *fetchRun) isCurrentTerm(item *mergedItem, start time.Time) bool {
	raw := item.deadlineRaw
	if raw == "" || raw == "0" {
		raw = item.createRaw
	}
	t, err := parseUnixSeconds(raw)
	if err != nil || t == nil {
		return true
	}
	return !t.Before(start)
}

func (r *fetchRun) registerClasses(classes []stduntClass) {
	for _, cls := range classes {
		if cls.ClassID == "" || cls.IsDel == "1" {
			continue
		}
		r.addCourse(cls.ClassID, cls.Class)
	}
}

func (r *fetchRun) addCourse(id, name string) *courseRef {
	ref := &courseRef{id: id, name: strings.TrimSpace(name)}
	r.courses[id] = ref
	if key := entity.NormalizeTitle(name); key != "" {
		if _, ok := r.byName[key]; !ok {
			r.byName[key] = ref
		}
	}
	r.order = append(r.order, ref)
	return ref
}

// courseByID 用详情里的 class_id 定位班级；班级表里没有就按详情补一条
func (r *fetchRun) courseByID(classID, className string) *courseRef {
	classID = strings.TrimSpace(classID)
	if classID == "" {
		return r.unknownCourse()
	}
	if ref, ok := r.courses[classID]; ok {
		return ref
	}
	name := strings.TrimSpace(className)
	if name == "" {
		name = "班级 " + classID
	}
	ref := r.addCourse(classID, name)
	r.warn(entity.ScopeCourse, classID, entity.IssueParse,
		fmt.Sprintf("班级 %s 不在班级列表中，已按作业详情补建", classID))
	return ref
}

// courseByName 用班级名配对。已完成列表只有名字，没有 class_id
func (r *fetchRun) courseByName(name string) *courseRef {
	key := entity.NormalizeTitle(name)
	if key == "" {
		return r.unknownCourse()
	}
	if ref, ok := r.byName[key]; ok {
		return ref
	}
	id := classKeyPrefix + key
	if ref, ok := r.courses[id]; ok {
		return ref
	}
	ref := r.addCourse(id, name)
	r.warn(entity.ScopeCourse, id, entity.IssueParse,
		fmt.Sprintf("班级 %q 未出现在班级列表中，已单独建课", key))
	return ref
}

func (r *fetchRun) unknownCourse() *courseRef {
	if ref, ok := r.courses[unknownClassID]; ok {
		return ref
	}
	ref := r.addCourse(unknownClassID, unknownClassName)
	r.warn(entity.ScopeCourse, unknownClassID, entity.IssueParse,
		fmt.Sprintf("有作业拿不到班级归属，已归入%s", unknownClassName))
	return ref
}

func (r *fetchRun) warn(scope entity.IssueScope, ref string, kind entity.IssueKind, message string) {
	r.result.Warnings = append(r.result.Warnings, entity.FetchIssue{
		Scope:   scope,
		Ref:     ref,
		Kind:    kind,
		Message: message,
	})
}

// observeServerNow 记下平台给出的当前时间，用于阶段换算
func (r *fetchRun) observeServerNow(sec int64) {
	if sec <= 0 || r.now != nil {
		return
	}
	t := time.Unix(sec, 0).In(shanghai)
	r.now = &t
}

func (c *Client) fetchPending(ctx context.Context, jar *cookieJar) (*pendingData, error) {
	env, err := c.fetchEnvelope(ctx, c.apiURL(url.Values{
		"c":  {"write"},
		"a":  {"waitComplete"},
		"bf": {"4"},
	}), jar)
	if err != nil {
		return nil, err
	}
	var data pendingData
	if err := decodeEnvelopeData(env, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *Client) fetchCompleted(ctx context.Context, jar *cookieJar) (*completedData, error) {
	env, err := c.fetchEnvelope(ctx, c.apiURL(url.Values{"c": {"write"}}), jar)
	if err != nil {
		return nil, err
	}
	var data completedData
	if err := decodeEnvelopeData(env, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *Client) fetchDetail(ctx context.Context, detailURL string, jar *cookieJar) (*detailData, error) {
	env, err := c.fetchEnvelope(ctx, detailURL, jar)
	if err != nil {
		return nil, err
	}
	var data detailData
	if err := decodeEnvelopeData(env, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// decodeEnvelopeData 解析信封里的 data；缺省或 null 都当空结果，不是错误
func decodeEnvelopeData(env *envelope, out any) error {
	raw := bytes.TrimSpace(env.Data)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: 响应结构无法解析: %v", entity.ErrPlatformChanged, err)
	}
	return nil
}

// mergeItems 按 request_id 合并两个列表：未完成优先，两边都有算未提交
func (c *Client) mergeItems(pending []requestInfo, completed []essayItem) []*mergedItem {
	items := make([]*mergedItem, 0, len(pending)+len(completed))
	index := make(map[string]*mergedItem, len(pending)+len(completed))

	for _, p := range pending {
		if p.RequestID == "" {
			continue
		}
		if _, ok := index[p.RequestID]; ok {
			continue
		}
		item := &mergedItem{
			requestID:   p.RequestID,
			status:      entity.StatusUnsubmitted,
			title:       p.EssayTitle,
			deadlineRaw: p.ValidEndTime,
			createRaw:   p.CreateTime,
			detailURL:   c.writeDetailURL(p.RequestID),
		}
		index[p.RequestID] = item
		items = append(items, item)
	}

	// 同一个 request 出现多条时取最新一版。返回哪一版没有验证过，
	// 按提交时间、再按版本号挑选比「取第一条」更保守
	best := make(map[string]essayItem, len(completed))
	for _, e := range completed {
		if e.RequestID == "" {
			continue
		}
		if prev, ok := best[e.RequestID]; !ok || newerEssay(e, prev) {
			best[e.RequestID] = e
		}
	}

	seen := make(map[string]bool, len(completed))
	for _, e := range completed {
		if e.RequestID == "" || seen[e.RequestID] {
			continue
		}
		seen[e.RequestID] = true
		chosen := best[e.RequestID]

		if item, ok := index[e.RequestID]; ok {
			// 未完成列表优先级更高，只补上班级名
			if item.stuClass == "" {
				item.stuClass = chosen.StuClass
			}
			continue
		}
		item := &mergedItem{
			requestID:   e.RequestID,
			status:      entity.StatusSubmitted,
			title:       chosen.Title,
			essayID:     chosen.EssayID,
			stuClass:    chosen.StuClass,
			deadlineRaw: chosen.ValidEndTime,
			createRaw:   chosen.CreateTime,
			detailURL:   c.viewDetailURL(chosen.EssayID, e.RequestID),
		}
		index[e.RequestID] = item
		items = append(items, item)
	}
	return items
}

// newerEssay 比较同一 request 的两版作文：提交时间大的优先，其次版本号
func newerEssay(a, b essayItem) bool {
	at, bt := numericOrZero(a.Ctime), numericOrZero(b.Ctime)
	if at != bt {
		return at > bt
	}
	return numericOrZero(a.Version) > numericOrZero(b.Version)
}

func numericOrZero(raw string) int64 {
	s := strings.TrimSpace(raw)
	var out int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		out = out*10 + int64(r-'0')
	}
	return out
}

func (c *Client) writeDetailURL(requestID string) string {
	return c.apiURL(url.Values{
		"c":   {"v2"},
		"a":   {"write"},
		"rid": {requestID},
	})
}

func (c *Client) viewDetailURL(essayID, requestID string) string {
	return c.apiURL(url.Values{
		"c":   {"v2"},
		"a":   {"view"},
		"eid": {essayID},
		"rid": {requestID},
	})
}

var htmlTagRe = regexp.MustCompile(`(?s)<[^>]*>`)

// stripHTML 把题干 HTML 变成纯文本。正文全文不取，只取题干
func stripHTML(s string) string {
	if s == "" {
		return ""
	}
	text := htmlTagRe.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(html.UnescapeString(text)), " ")
}
