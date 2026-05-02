package loader_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/registry"
	"odin-os/internal/registry/loader"
)

func TestScanDirInfersKinds(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills", "triage.md"), sampleSkillMarkdown("triage-skill"))
	writeFile(t, filepath.Join(root, "commands", "status.md"), sampleCommandMarkdown("status-command"))

	files, err := loader.ScanDir(root)
	if err != nil {
		t.Fatalf("ScanDir() error = %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("ScanDir() files = %d, want 2", len(files))
	}

	if files[0].ExpectedKind != registry.KindCommand {
		t.Fatalf("files[0].ExpectedKind = %q, want %q", files[0].ExpectedKind, registry.KindCommand)
	}

	if files[1].ExpectedKind != registry.KindSkill {
		t.Fatalf("files[1].ExpectedKind = %q, want %q", files[1].ExpectedKind, registry.KindSkill)
	}
}

func TestLoadDirCompilesValidFilesAndReportsInvalidOnes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills", "triage.md"), sampleSkillMarkdown("triage-skill"))
	writeFile(t, filepath.Join(root, "skills", "broken.md"), brokenSkillMarkdown("broken-skill"))

	snapshot, err := loader.LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}

	if len(snapshot.Items) != 1 {
		t.Fatalf("snapshot.Items = %d, want 1", len(snapshot.Items))
	}

	if snapshot.Items[0].Key != "triage-skill" {
		t.Fatalf("snapshot.Items[0].Key = %q, want %q", snapshot.Items[0].Key, "triage-skill")
	}

	if len(snapshot.Diagnostics) == 0 {
		t.Fatal("snapshot.Diagnostics = 0, want at least 1")
	}
}

func TestLoadDirLoadsRepositoryExamples(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "..", "registry"))

	snapshot, err := loader.LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}

	if len(snapshot.Diagnostics) != 0 {
		t.Fatalf("snapshot.Diagnostics = %v, want none", snapshot.Diagnostics)
	}

	wantKeys := []string{
		"flica-annual-vacation",
		"flica-fcfs-bid",
		"flica-schedule",
		"flica-seniority-bid",
		"flica-tradeboard",
		"flica-tradeboard-split-post",
		"project-intake",
	}
	loadedKeys := make(map[string]bool, len(snapshot.Items))
	for _, item := range snapshot.Items {
		loadedKeys[item.Key] = true
	}
	for _, key := range wantKeys {
		if !loadedKeys[key] {
			t.Fatalf("snapshot.Items missing %q", key)
		}
	}
}

func TestLoadDirLoadsUniversalIntakeAgents(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "..", "registry"))

	snapshot, err := loader.LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}

	if len(snapshot.Diagnostics) != 0 {
		t.Fatalf("snapshot.Diagnostics = %v, want none", snapshot.Diagnostics)
	}

	wantAgents := []string{
		"universal-os-orchestrator",
		"capture-agent",
		"classifier-agent",
		"deduper-agent",
		"priority-agent",
		"urgency-importance-judge-agent",
		"router-agent",
		"spec-task-builder-agent",
		"universal-ticket-generator-agent",
		"software-feature-ticket-builder-agent",
		"bug-report-builder-agent",
		"research-ticket-builder-agent",
		"writing-task-builder-agent",
		"plan-first-execution-agent",
		"subagent-delegation-planner-agent",
		"task-splitter-agent",
		"project-spec-builder-agent",
		"personal-project-builder-agent",
		"review-agent",
		"chief-of-staff-agent",
		"system-memory-curator-agent",
		"voice-note-cleaner-agent",
		"email-to-task-extractor-agent",
		"visual-intake-agent",
		"meeting-notes-intake-agent",
	}
	for _, key := range wantAgents {
		item, ok := snapshot.ByKey[key]
		if !ok {
			t.Fatalf("snapshot.ByKey missing %q", key)
		}
		if item.Kind != registry.KindAgent {
			t.Fatalf("%s kind = %q, want %q", key, item.Kind, registry.KindAgent)
		}
		if !containsString(item.Tags, "universal-intake") {
			t.Fatalf("%s tags = %v, want universal-intake", key, item.Tags)
		}
	}

	orchestrator := snapshot.ByKey["universal-os-orchestrator"]
	orchestratorContract := strings.Join([]string{
		orchestrator.Sections[registry.SectionPurpose],
		orchestrator.Sections[registry.SectionWhenToUse],
		orchestrator.Sections[registry.SectionInputs],
		orchestrator.Sections[registry.SectionProcedure],
		orchestrator.Sections[registry.SectionOutputs],
		orchestrator.Sections[registry.SectionConstraints],
		orchestrator.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredContract := []string{
		"task",
		"project",
		"idea",
		"bug",
		"feature request",
		"personal admin",
		"calendar item",
		"research request",
		"writing request",
		"coding request",
		"learning goal",
		"health or wellbeing item",
		"finance/admin item",
		"household item",
		"waiting-for item",
		"archive/reference item",
		"unclear",
		"cleaned summary",
		"human approval is required",
		"specialist agent",
		"Never execute high-risk actions directly",
		"Never create implementation tasks from vague ideas",
		"create a clarification task instead of guessing",
	}
	for _, required := range requiredContract {
		if !strings.Contains(orchestratorContract, required) {
			t.Fatalf("universal orchestrator body missing %q", required)
		}
	}

	capture := snapshot.ByKey["capture-agent"]
	captureContract := strings.Join([]string{
		capture.Title,
		capture.Summary,
		capture.Sections[registry.SectionPurpose],
		capture.Sections[registry.SectionWhenToUse],
		capture.Sections[registry.SectionInputs],
		capture.Sections[registry.SectionProcedure],
		capture.Sections[registry.SectionOutputs],
		capture.Sections[registry.SectionConstraints],
		capture.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredCaptureContract := []string{
		"Universal Inbox Capture Agent",
		"{{raw_input}}",
		"{{source}}",
		"{{timestamp}}",
		"structured capture record",
		"title",
		"one-sentence summary",
		"original intent",
		"possible project or life area",
		"actionability: actionable, reference, someday, unclear",
		"extracted deadlines",
		"extracted people",
		"extracted links or resources",
		"emotional tone, if relevant",
		"recommended next processing step",
		"Do not create tasks yet",
		"Do not assume missing details",
	}
	for _, required := range requiredCaptureContract {
		if !strings.Contains(captureContract, required) {
			t.Fatalf("capture agent body missing %q", required)
		}
	}

	classifier := snapshot.ByKey["classifier-agent"]
	classifierContract := strings.Join([]string{
		classifier.Title,
		classifier.Summary,
		classifier.Sections[registry.SectionPurpose],
		classifier.Sections[registry.SectionWhenToUse],
		classifier.Sections[registry.SectionInputs],
		classifier.Sections[registry.SectionProcedure],
		classifier.Sections[registry.SectionOutputs],
		classifier.Sections[registry.SectionConstraints],
		classifier.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredClassifierContract := []string{
		"Task Classifier",
		"{{raw_input}}",
		"Use exactly one primary category",
		"task",
		"project",
		"idea",
		"bug",
		"feature request",
		"research",
		"writing",
		"personal admin",
		"calendar",
		"email",
		"learning",
		"household",
		"finance",
		"health",
		"waiting-for",
		"archive",
		"unclear",
		"confidence score from 0 to 100",
		"secondary categories",
		"reason for classification",
		"recommended next agent",
		"whether this needs clarification",
	}
	for _, required := range requiredClassifierContract {
		if !strings.Contains(classifierContract, required) {
			t.Fatalf("classifier agent body missing %q", required)
		}
	}

	deduper := snapshot.ByKey["deduper-agent"]
	deduperContract := strings.Join([]string{
		deduper.Title,
		deduper.Summary,
		deduper.Sections[registry.SectionPurpose],
		deduper.Sections[registry.SectionWhenToUse],
		deduper.Sections[registry.SectionInputs],
		deduper.Sections[registry.SectionProcedure],
		deduper.Sections[registry.SectionOutputs],
		deduper.Sections[registry.SectionConstraints],
		deduper.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredDeduperContract := []string{
		"Duplicate Detector",
		"{{raw_input}}",
		"{{knowledge_base_context}}",
		"existing tasks, projects, notes, and tickets",
		"duplicate_found: yes/no",
		"likely matching item",
		"confidence score",
		"whether to merge, update, link, or create new",
		"suggested merged title",
		"suggested merged summary",
		"details unique to the new item",
	}
	for _, required := range requiredDeduperContract {
		if !strings.Contains(deduperContract, required) {
			t.Fatalf("deduper agent body missing %q", required)
		}
	}

	priority := snapshot.ByKey["priority-agent"]
	priorityContract := strings.Join([]string{
		priority.Title,
		priority.Summary,
		priority.Sections[registry.SectionPurpose],
		priority.Sections[registry.SectionWhenToUse],
		priority.Sections[registry.SectionInputs],
		priority.Sections[registry.SectionProcedure],
		priority.Sections[registry.SectionOutputs],
		priority.Sections[registry.SectionConstraints],
		priority.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredPriorityContract := []string{
		"Priority Scorer",
		"{{raw_input}}",
		"impact: 0 to 5",
		"urgency: 0 to 5",
		"effort: 0 to 5",
		"strategic alignment: 0 to 5",
		"risk if ignored: 0 to 5",
		"energy required: 0 to 5",
		"dependency value: 0 to 5",
		"total priority score",
		"recommended priority: low, medium, high, critical",
		"recommended timing: today, this week, this month, later, delete",
		"reasoning",
		"warning if the item is emotionally loud but strategically weak",
		"Do not rank everything as high priority",
		"panic with bullet points",
	}
	for _, required := range requiredPriorityContract {
		if !strings.Contains(priorityContract, required) {
			t.Fatalf("priority agent body missing %q", required)
		}
	}

	urgencyImportanceJudge := snapshot.ByKey["urgency-importance-judge-agent"]
	urgencyImportanceJudgeContract := strings.Join([]string{
		urgencyImportanceJudge.Title,
		urgencyImportanceJudge.Summary,
		urgencyImportanceJudge.Sections[registry.SectionPurpose],
		urgencyImportanceJudge.Sections[registry.SectionWhenToUse],
		urgencyImportanceJudge.Sections[registry.SectionInputs],
		urgencyImportanceJudge.Sections[registry.SectionProcedure],
		urgencyImportanceJudge.Sections[registry.SectionOutputs],
		urgencyImportanceJudge.Sections[registry.SectionConstraints],
		urgencyImportanceJudge.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredUrgencyImportanceJudgeContract := []string{
		"Urgency vs Importance Judge",
		"{{raw_input}}",
		"urgent and important",
		"important but not urgent",
		"urgent but not important",
		"neither urgent nor important",
		"classification",
		"reason",
		"consequence of delaying",
		"consequence of doing now",
		"recommended next step",
		"whether to schedule, delegate, defer, delete, or do immediately",
	}
	for _, required := range requiredUrgencyImportanceJudgeContract {
		if !strings.Contains(urgencyImportanceJudgeContract, required) {
			t.Fatalf("urgency importance judge agent body missing %q", required)
		}
	}

	universalTicketGenerator := snapshot.ByKey["universal-ticket-generator-agent"]
	universalTicketGeneratorContract := strings.Join([]string{
		universalTicketGenerator.Title,
		universalTicketGenerator.Summary,
		universalTicketGenerator.Sections[registry.SectionPurpose],
		universalTicketGenerator.Sections[registry.SectionWhenToUse],
		universalTicketGenerator.Sections[registry.SectionInputs],
		universalTicketGenerator.Sections[registry.SectionProcedure],
		universalTicketGenerator.Sections[registry.SectionOutputs],
		universalTicketGenerator.Sections[registry.SectionConstraints],
		universalTicketGenerator.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredUniversalTicketGeneratorContract := []string{
		"Universal Ticket Generator",
		"{{raw_input}}",
		"Create a ticket from this input",
		"title",
		"type",
		"project or area",
		"problem statement",
		"desired outcome",
		"acceptance criteria",
		"non-goals",
		"constraints",
		"dependencies",
		"risks",
		"estimated effort",
		"recommended owner or agent",
		"approval status",
		"Do not create implementation instructions unless the task is approved for execution",
	}
	for _, required := range requiredUniversalTicketGeneratorContract {
		if !strings.Contains(universalTicketGeneratorContract, required) {
			t.Fatalf("universal ticket generator agent body missing %q", required)
		}
	}

	softwareFeatureTicketBuilder := snapshot.ByKey["software-feature-ticket-builder-agent"]
	softwareFeatureTicketBuilderContract := strings.Join([]string{
		softwareFeatureTicketBuilder.Title,
		softwareFeatureTicketBuilder.Summary,
		softwareFeatureTicketBuilder.Sections[registry.SectionPurpose],
		softwareFeatureTicketBuilder.Sections[registry.SectionWhenToUse],
		softwareFeatureTicketBuilder.Sections[registry.SectionInputs],
		softwareFeatureTicketBuilder.Sections[registry.SectionProcedure],
		softwareFeatureTicketBuilder.Sections[registry.SectionOutputs],
		softwareFeatureTicketBuilder.Sections[registry.SectionConstraints],
		softwareFeatureTicketBuilder.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredSoftwareFeatureTicketBuilderContract := []string{
		"Software Feature Ticket Builder",
		"{{raw_input}}",
		"Create a software feature ticket from this input",
		"feature title",
		"user story",
		"problem",
		"proposed solution",
		"acceptance criteria",
		"non-goals",
		"affected users",
		"affected systems",
		"data model impact",
		"API impact",
		"UI impact",
		"security/privacy risks",
		"test requirements",
		"documentation requirements",
		"recommended implementation phases",
		"Do not write code",
		"Do not assume architecture details that are not provided",
	}
	for _, required := range requiredSoftwareFeatureTicketBuilderContract {
		if !strings.Contains(softwareFeatureTicketBuilderContract, required) {
			t.Fatalf("software feature ticket builder agent body missing %q", required)
		}
	}

	bugReportBuilder := snapshot.ByKey["bug-report-builder-agent"]
	bugReportBuilderContract := strings.Join([]string{
		bugReportBuilder.Title,
		bugReportBuilder.Summary,
		bugReportBuilder.Sections[registry.SectionPurpose],
		bugReportBuilder.Sections[registry.SectionWhenToUse],
		bugReportBuilder.Sections[registry.SectionInputs],
		bugReportBuilder.Sections[registry.SectionProcedure],
		bugReportBuilder.Sections[registry.SectionOutputs],
		bugReportBuilder.Sections[registry.SectionConstraints],
		bugReportBuilder.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredBugReportBuilderContract := []string{
		"Bug Report Builder",
		"{{raw_input}}",
		"Turn this bug-related input into a structured bug report",
		"bug title",
		"observed behavior",
		"expected behavior",
		"steps to reproduce",
		"affected user or system",
		"frequency",
		"severity",
		"possible cause",
		"logs or screenshots needed",
		"workaround, if known",
		"test that should fail before the fix",
		"recommended owner or agent",
	}
	for _, required := range requiredBugReportBuilderContract {
		if !strings.Contains(bugReportBuilderContract, required) {
			t.Fatalf("bug report builder agent body missing %q", required)
		}
	}

	researchTicketBuilder := snapshot.ByKey["research-ticket-builder-agent"]
	researchTicketBuilderContract := strings.Join([]string{
		researchTicketBuilder.Title,
		researchTicketBuilder.Summary,
		researchTicketBuilder.Sections[registry.SectionPurpose],
		researchTicketBuilder.Sections[registry.SectionWhenToUse],
		researchTicketBuilder.Sections[registry.SectionInputs],
		researchTicketBuilder.Sections[registry.SectionProcedure],
		researchTicketBuilder.Sections[registry.SectionOutputs],
		researchTicketBuilder.Sections[registry.SectionConstraints],
		researchTicketBuilder.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredResearchTicketBuilderContract := []string{
		"Research Ticket Builder",
		"{{raw_input}}",
		"Turn this input into a research ticket",
		"research question",
		"why this matters",
		"sources to check",
		"freshness requirement",
		"decision this research supports",
		"output format",
		"deadline",
		"risks of using outdated information",
		"what would change the conclusion",
		"recommended next step",
		"If the topic is time-sensitive, require current sources",
	}
	for _, required := range requiredResearchTicketBuilderContract {
		if !strings.Contains(researchTicketBuilderContract, required) {
			t.Fatalf("research ticket builder agent body missing %q", required)
		}
	}

	writingTaskBuilder := snapshot.ByKey["writing-task-builder-agent"]
	writingTaskBuilderContract := strings.Join([]string{
		writingTaskBuilder.Title,
		writingTaskBuilder.Summary,
		writingTaskBuilder.Sections[registry.SectionPurpose],
		writingTaskBuilder.Sections[registry.SectionWhenToUse],
		writingTaskBuilder.Sections[registry.SectionInputs],
		writingTaskBuilder.Sections[registry.SectionProcedure],
		writingTaskBuilder.Sections[registry.SectionOutputs],
		writingTaskBuilder.Sections[registry.SectionConstraints],
		writingTaskBuilder.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredWritingTaskBuilderContract := []string{
		"Writing Task Builder",
		"{{raw_input}}",
		"Create a writing brief from this input",
		"working title",
		"purpose",
		"audience",
		"format",
		"desired tone",
		"key points",
		"sources or references needed",
		"length target",
		"call to action",
		"deadline",
		"first draft instructions",
	}
	for _, required := range requiredWritingTaskBuilderContract {
		if !strings.Contains(writingTaskBuilderContract, required) {
			t.Fatalf("writing task builder agent body missing %q", required)
		}
	}

	planFirstExecution := snapshot.ByKey["plan-first-execution-agent"]
	planFirstExecutionContract := strings.Join([]string{
		planFirstExecution.Title,
		planFirstExecution.Summary,
		planFirstExecution.Sections[registry.SectionPurpose],
		planFirstExecution.Sections[registry.SectionWhenToUse],
		planFirstExecution.Sections[registry.SectionInputs],
		planFirstExecution.Sections[registry.SectionProcedure],
		planFirstExecution.Sections[registry.SectionOutputs],
		planFirstExecution.Sections[registry.SectionConstraints],
		planFirstExecution.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredPlanFirstExecutionContract := []string{
		"Plan-First Execution Agent",
		"{{raw_input}}",
		"Given this approved task",
		"Create an execution plan before taking action",
		"objective",
		"assumptions",
		"required context",
		"steps",
		"tools needed",
		"risks",
		"approval gates",
		"expected output",
		"definition of done",
		"rollback or undo plan, if applicable",
		"Do not execute until the plan is approved if risk is medium, high, or critical",
	}
	for _, required := range requiredPlanFirstExecutionContract {
		if !strings.Contains(planFirstExecutionContract, required) {
			t.Fatalf("plan-first execution agent body missing %q", required)
		}
	}

	subagentDelegationPlanner := snapshot.ByKey["subagent-delegation-planner-agent"]
	subagentDelegationPlannerContract := strings.Join([]string{
		subagentDelegationPlanner.Title,
		subagentDelegationPlanner.Summary,
		subagentDelegationPlanner.Sections[registry.SectionPurpose],
		subagentDelegationPlanner.Sections[registry.SectionWhenToUse],
		subagentDelegationPlanner.Sections[registry.SectionInputs],
		subagentDelegationPlanner.Sections[registry.SectionProcedure],
		subagentDelegationPlanner.Sections[registry.SectionOutputs],
		subagentDelegationPlanner.Sections[registry.SectionConstraints],
		subagentDelegationPlanner.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredSubagentDelegationPlannerContract := []string{
		"Subagent Delegation Planner",
		"{{raw_input}}",
		"Given this task",
		"Decide whether subagents are needed",
		"Research Agent",
		"Planner Agent",
		"Software Architect Agent",
		"Coding Agent",
		"Code Review Agent",
		"Security Agent",
		"Writing Agent",
		"Editor Agent",
		"Email Agent",
		"Calendar Agent",
		"Personal Admin Agent",
		"Finance Admin Agent",
		"Household Agent",
		"Learning Coach Agent",
		"Decision Analyst Agent",
		"whether subagents are needed",
		"selected subagents",
		"task for each subagent",
		"sequence or parallel execution",
		"required shared context",
		"consolidation method",
		"final reviewer",
	}
	for _, required := range requiredSubagentDelegationPlannerContract {
		if !strings.Contains(subagentDelegationPlannerContract, required) {
			t.Fatalf("subagent delegation planner agent body missing %q", required)
		}
	}

	taskSplitter := snapshot.ByKey["task-splitter-agent"]
	taskSplitterContract := strings.Join([]string{
		taskSplitter.Title,
		taskSplitter.Summary,
		taskSplitter.Sections[registry.SectionPurpose],
		taskSplitter.Sections[registry.SectionWhenToUse],
		taskSplitter.Sections[registry.SectionInputs],
		taskSplitter.Sections[registry.SectionProcedure],
		taskSplitter.Sections[registry.SectionOutputs],
		taskSplitter.Sections[registry.SectionConstraints],
		taskSplitter.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredTaskSplitterContract := []string{
		"Task Splitter",
		"{{raw_input}}",
		"Break this task or project into smaller tasks",
		"recommended task list",
		"order of execution",
		"dependencies",
		"estimated effort per task",
		"which tasks can be automated",
		"which tasks require human review",
		"first task to start with",
		"tasks that should be deferred",
	}
	for _, required := range requiredTaskSplitterContract {
		if !strings.Contains(taskSplitterContract, required) {
			t.Fatalf("task splitter agent body missing %q", required)
		}
	}

	projectSpecBuilder := snapshot.ByKey["project-spec-builder-agent"]
	projectSpecBuilderContract := strings.Join([]string{
		projectSpecBuilder.Title,
		projectSpecBuilder.Summary,
		projectSpecBuilder.Sections[registry.SectionPurpose],
		projectSpecBuilder.Sections[registry.SectionWhenToUse],
		projectSpecBuilder.Sections[registry.SectionInputs],
		projectSpecBuilder.Sections[registry.SectionProcedure],
		projectSpecBuilder.Sections[registry.SectionOutputs],
		projectSpecBuilder.Sections[registry.SectionConstraints],
		projectSpecBuilder.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredProjectSpecBuilderContract := []string{
		"Project Spec Builder",
		"{{raw_input}}",
		"Turn this idea into a project spec",
		"project name",
		"one-sentence purpose",
		"target user or beneficiary",
		"problem",
		"success criteria",
		"scope",
		"non-goals",
		"phases",
		"required resources",
		"risks",
		"first milestone",
		"first next action",
		"clarification checklist",
		"If the idea is too vague",
	}
	for _, required := range requiredProjectSpecBuilderContract {
		if !strings.Contains(projectSpecBuilderContract, required) {
			t.Fatalf("project spec builder agent body missing %q", required)
		}
	}

	personalProjectBuilder := snapshot.ByKey["personal-project-builder-agent"]
	personalProjectBuilderContract := strings.Join([]string{
		personalProjectBuilder.Title,
		personalProjectBuilder.Summary,
		personalProjectBuilder.Sections[registry.SectionPurpose],
		personalProjectBuilder.Sections[registry.SectionWhenToUse],
		personalProjectBuilder.Sections[registry.SectionInputs],
		personalProjectBuilder.Sections[registry.SectionProcedure],
		personalProjectBuilder.Sections[registry.SectionOutputs],
		personalProjectBuilder.Sections[registry.SectionConstraints],
		personalProjectBuilder.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredPersonalProjectBuilderContract := []string{
		"Personal Project Builder",
		"{{raw_input}}",
		"Turn this personal goal into a structured project",
		"goal statement",
		"why it matters",
		"measurable outcome",
		"deadline or review date",
		"milestones",
		"weekly actions",
		"daily habits, if relevant",
		"blockers",
		"support needed",
		"first action under 15 minutes",
		"Keep the plan realistic",
		"Do not create a motivational poster disguised as a plan",
	}
	for _, required := range requiredPersonalProjectBuilderContract {
		if !strings.Contains(personalProjectBuilderContract, required) {
			t.Fatalf("personal project builder agent body missing %q", required)
		}
	}

	voiceNoteCleaner := snapshot.ByKey["voice-note-cleaner-agent"]
	voiceNoteCleanerContract := strings.Join([]string{
		voiceNoteCleaner.Title,
		voiceNoteCleaner.Summary,
		voiceNoteCleaner.Sections[registry.SectionPurpose],
		voiceNoteCleaner.Sections[registry.SectionWhenToUse],
		voiceNoteCleaner.Sections[registry.SectionInputs],
		voiceNoteCleaner.Sections[registry.SectionProcedure],
		voiceNoteCleaner.Sections[registry.SectionOutputs],
		voiceNoteCleaner.Sections[registry.SectionConstraints],
		voiceNoteCleaner.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredVoiceNoteCleanerContract := []string{
		"Voice Note Cleaner",
		"{{raw_input}}",
		"filler",
		"repetition",
		"half-ideas",
		"corrections",
		"unrelated thoughts",
		"cleaned summary",
		"separate ideas",
		"possible tasks",
		"possible projects",
		"questions that need clarification",
		"anything that should be archived as reference",
		"anything emotionally important",
		"recommended next action",
		"Do not over-interpret unclear statements",
	}
	for _, required := range requiredVoiceNoteCleanerContract {
		if !strings.Contains(voiceNoteCleanerContract, required) {
			t.Fatalf("voice note cleaner agent body missing %q", required)
		}
	}

	emailToTaskExtractor := snapshot.ByKey["email-to-task-extractor-agent"]
	emailToTaskExtractorContract := strings.Join([]string{
		emailToTaskExtractor.Title,
		emailToTaskExtractor.Summary,
		emailToTaskExtractor.Sections[registry.SectionPurpose],
		emailToTaskExtractor.Sections[registry.SectionWhenToUse],
		emailToTaskExtractor.Sections[registry.SectionInputs],
		emailToTaskExtractor.Sections[registry.SectionProcedure],
		emailToTaskExtractor.Sections[registry.SectionOutputs],
		emailToTaskExtractor.Sections[registry.SectionConstraints],
		emailToTaskExtractor.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredEmailToTaskExtractorContract := []string{
		"Email-to-Task Extractor",
		"{{raw_input}}",
		"email or email thread",
		"who sent it",
		"what they want",
		"what I owe them",
		"what they owe me",
		"deadlines",
		"meetings or scheduling needs",
		"attachments or links to review",
		"reply required: yes/no",
		"proposed reply summary",
		"tasks to create",
		"do now",
		"schedule",
		"delegate",
		"waiting-for",
		"archive",
		"unclear",
		"Do not draft or send a reply unless explicitly requested",
	}
	for _, required := range requiredEmailToTaskExtractorContract {
		if !strings.Contains(emailToTaskExtractorContract, required) {
			t.Fatalf("email-to-task extractor agent body missing %q", required)
		}
	}

	visualIntake := snapshot.ByKey["visual-intake-agent"]
	visualIntakeContract := strings.Join([]string{
		visualIntake.Title,
		visualIntake.Summary,
		visualIntake.Sections[registry.SectionPurpose],
		visualIntake.Sections[registry.SectionWhenToUse],
		visualIntake.Sections[registry.SectionInputs],
		visualIntake.Sections[registry.SectionProcedure],
		visualIntake.Sections[registry.SectionOutputs],
		visualIntake.Sections[registry.SectionConstraints],
		visualIntake.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredVisualIntakeContract := []string{
		"Visual Intake Agent",
		"provided image, screenshot, whiteboard, or handwritten note",
		"visible content summary",
		"extracted text",
		"possible tasks",
		"possible decisions",
		"possible project links",
		"risks or missing context",
		"recommended next step",
		"If the image is unclear, say exactly what is unreadable",
		"Do not invent text or details",
	}
	for _, required := range requiredVisualIntakeContract {
		if !strings.Contains(visualIntakeContract, required) {
			t.Fatalf("visual intake agent body missing %q", required)
		}
	}

	meetingNotesIntake := snapshot.ByKey["meeting-notes-intake-agent"]
	meetingNotesIntakeContract := strings.Join([]string{
		meetingNotesIntake.Title,
		meetingNotesIntake.Summary,
		meetingNotesIntake.Sections[registry.SectionPurpose],
		meetingNotesIntake.Sections[registry.SectionWhenToUse],
		meetingNotesIntake.Sections[registry.SectionInputs],
		meetingNotesIntake.Sections[registry.SectionProcedure],
		meetingNotesIntake.Sections[registry.SectionOutputs],
		meetingNotesIntake.Sections[registry.SectionConstraints],
		meetingNotesIntake.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredMeetingNotesIntakeContract := []string{
		"Meeting Notes Intake Agent",
		"{{raw_input}}",
		"meeting notes or transcript",
		"meeting purpose",
		"key decisions",
		"action items",
		"owners",
		"deadlines",
		"unresolved questions",
		"risks",
		"follow-up messages needed",
		"calendar items needed",
		"documents or tickets to create",
		"someone should",
		"we need to",
		"Flag vague ownership",
	}
	for _, required := range requiredMeetingNotesIntakeContract {
		if !strings.Contains(meetingNotesIntakeContract, required) {
			t.Fatalf("meeting notes intake agent body missing %q", required)
		}
	}

	chiefOfStaff := snapshot.ByKey["chief-of-staff-agent"]
	chiefOfStaffContract := strings.Join([]string{
		chiefOfStaff.Sections[registry.SectionPurpose],
		chiefOfStaff.Sections[registry.SectionWhenToUse],
		chiefOfStaff.Sections[registry.SectionInputs],
		chiefOfStaff.Sections[registry.SectionProcedure],
		chiefOfStaff.Sections[registry.SectionOutputs],
		chiefOfStaff.Sections[registry.SectionConstraints],
		chiefOfStaff.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredBriefContract := []string{
		"active tasks",
		"projects",
		"calendar context",
		"waiting-for items",
		"recent inbox captures",
		"deadlines",
		"top 3 priorities",
		"urgent deadlines",
		"quick wins under 15 minutes",
		"blocked items",
		"waiting-for follow-ups",
		"decisions I need to make",
		"tasks that should be delegated to other agents",
		"tasks that should be deleted or deferred",
		"one recommended focus block",
		"one warning about overcommitment",
		"Do not inflate trivial tasks into strategic initiatives",
	}
	for _, required := range requiredBriefContract {
		if !strings.Contains(chiefOfStaffContract, required) {
			t.Fatalf("chief of staff agent body missing %q", required)
		}
	}

	router := snapshot.ByKey["router-agent"]
	routerContract := strings.Join([]string{
		router.Sections[registry.SectionPurpose],
		router.Sections[registry.SectionWhenToUse],
		router.Sections[registry.SectionInputs],
		router.Sections[registry.SectionProcedure],
		router.Sections[registry.SectionOutputs],
		router.Sections[registry.SectionConstraints],
		router.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredRouterContract := []string{
		"Project Manager Agent",
		"Software Planner Agent",
		"Coding Agent",
		"Code Review Agent",
		"Research Agent",
		"Personal Admin Agent",
		"Calendar Agent",
		"Email Agent",
		"Writing Agent",
		"Learning Coach Agent",
		"Finance Admin Agent",
		"Household Agent",
		"Health and Wellbeing Support Agent",
		"Travel Agent",
		"Document Summarizer Agent",
		"Decision Support Agent",
		"Archive Agent",
		"selected agent",
		"reason",
		"required context",
		"required tools",
		"whether subagents are needed",
		"whether approval is needed before action",
	}
	for _, required := range requiredRouterContract {
		if !strings.Contains(routerContract, required) {
			t.Fatalf("router agent body missing %q", required)
		}
	}

	memoryCurator := snapshot.ByKey["system-memory-curator-agent"]
	memoryCuratorContract := strings.Join([]string{
		memoryCurator.Sections[registry.SectionPurpose],
		memoryCurator.Sections[registry.SectionWhenToUse],
		memoryCurator.Sections[registry.SectionInputs],
		memoryCurator.Sections[registry.SectionProcedure],
		memoryCurator.Sections[registry.SectionOutputs],
		memoryCurator.Sections[registry.SectionConstraints],
		memoryCurator.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredMemoryCuratorContract := []string{
		"completed interaction",
		"long-term memory",
		"project memory",
		"personal preference memory",
		"archived reference",
		"useful, stable, and likely to improve future decisions",
		"temporary moods",
		"one-off details",
		"sensitive information unless explicitly approved",
		"unverified facts",
		"guesses",
		"irrelevant chatter",
		"save_to_memory: yes/no",
		"memory_type",
		"exact memory text",
		"expiration or review date",
		"reason",
	}
	for _, required := range requiredMemoryCuratorContract {
		if !strings.Contains(memoryCuratorContract, required) {
			t.Fatalf("system memory curator agent body missing %q", required)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}

	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func sampleSkillMarkdown(key string) string {
	return `---
kind: skill
key: ` + key + `
title: Triage Skill
summary: Helps sort incoming work.
strictness: rigid
applies_to:
  - intake
---

# Triage Skill

## Purpose
Sort work.

## When to Use
When intake is noisy.

## Inputs
Work items.

## Procedure
Read and categorize.

## Outputs
Prioritized list.

## Constraints
Stay deterministic.

## Success Criteria
The queue is sorted.
`
}

func sampleCommandMarkdown(key string) string {
	return `---
kind: command
key: ` + key + `
title: Status Command
summary: Shows current status.
command: status
scopes:
  - global
aliases:
  - stat
---

# Status Command

## Purpose
Show status.

## When to Use
When an operator needs context.

## Inputs
Current scope.

## Procedure
Collect and display status.

## Outputs
Rendered status.

## Constraints
Avoid mutation.

## Success Criteria
The operator understands current state.
`
}

func brokenSkillMarkdown(key string) string {
	return `---
kind: skill
key: ` + key + `
title: Broken Skill
summary: Missing the Procedure section.
strictness: rigid
applies_to:
  - intake
---

# Broken Skill

## Purpose
Sort work.

## When to Use
When intake is noisy.

## Inputs
Work items.

## Outputs
Prioritized list.

## Constraints
Stay deterministic.

## Success Criteria
The queue is sorted.
`
}
