package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryCompactGradleBuildCleanSuccessOutput(t *testing.T) {
	t.Parallel()

	input := gradleBuildCleanSuccessFixture(36)
	out, ok := TryCompactGradle([]string{"gradlew", "build", "--parallel"}, []byte(input))
	if !ok || string(out) != "[gradle build] ok (36 actionable tasks: 36 executed)\n" {
		t.Fatalf("gradle clean success: ok=%v out=%q", ok, out)
	}
	if len(out) >= len(input) {
		t.Fatal("gradle clean success summary must be shorter than original output")
	}

	wrapped, ok := TryCompactBuildOutput([]string{"pnpm", "exec", "gradle", "build"}, []byte(input))
	if !ok || string(wrapped) != "[gradle build] ok (36 actionable tasks: 36 executed)\n" {
		t.Fatalf("wrapped gradle build clean success through build chain: ok=%v out=%q", ok, wrapped)
	}
}

func TestTryCompactGradleBuildCleanSuccessFailOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "deprecated success",
			input: gradleBuildCleanSuccessFixture(4) + "Deprecated Gradle features were used in this build.\n",
		},
		{
			name:  "warning success",
			input: gradleBuildCleanSuccessFixture(4) + "Warning: generated resource is stale\n",
		},
		{
			name:  "application log with success",
			input: "> Task :runGenerator\nGenerated production config\nBUILD SUCCESSFUL in 1s\n1 actionable task: 1 executed\n",
		},
		{
			name:  "source context with success",
			input: "> Task :compileJava\nsrc/Main.java:12: public class Main {}\nBUILD SUCCESSFUL in 1s\n1 actionable task: 1 executed\n",
		},
		{
			name:  "success without task summary",
			input: "BUILD SUCCESSFUL in 1s\n",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if out, ok := TryCompactGradle([]string{"gradle", "build"}, []byte(tt.input)); ok {
				t.Fatalf("unsafe gradle clean success compacted by strict parser: %q", out)
			}
			if out, ok := TryCompactBuildOutput([]string{"gradle", "build"}, []byte(tt.input)); ok {
				t.Fatalf("unsafe gradle clean success escaped through build chain: %q", out)
			}
		})
	}
}

func TestGradleBuildParserHelperEdges(t *testing.T) {
	t.Parallel()

	if suffix, ok := gradleCompactArgvSuffix(nil); ok || suffix != nil {
		t.Fatalf("nil gradle argv should miss: ok=%v suffix=%v", ok, suffix)
	}
	if gradleOutputContainsBuildSuccess("BUILD FAILED in 1s\n") {
		t.Fatal("failed build must not look like Gradle build success")
	}
	if gradleBuildActionableSummaryLine("1 actionable task: 1 cached") {
		t.Fatal("unknown Gradle actionable status must fail open")
	}
	if gradleBuildActionableSummaryLine("1 actionable task: 1 executed.") {
		t.Fatal("punctuated Gradle actionable summary must fail open")
	}
	if gradleBuildNeutralLine("Generated production config") {
		t.Fatal("unknown Gradle line must not be neutral")
	}

	upToDate := strings.Join([]string{
		"Gradle Daemon started in 1 s",
		"Configuration cache entry reused.",
		"> Task :compileJava UP-TO-DATE",
		"BUILD SUCCESSFUL in 1s",
		"1 actionable task: 1 up-to-date",
		"",
	}, "\n")
	out, ok := compactGradleBuildCleanOutput(upToDate, len(upToDate))
	if !ok || string(out) != "[gradle build] ok (1 actionable task: 1 up-to-date)\n" {
		t.Fatalf("up-to-date Gradle build summary: ok=%v out=%q", ok, out)
	}
	if out, ok := compactGradleBuildCleanOutput(upToDate, 1); ok || out != nil {
		t.Fatalf("non-positive-net Gradle output should fail open: ok=%v out=%q", ok, out)
	}
}

func gradleBuildCleanSuccessFixture(tasks int) string {
	var b strings.Builder
	b.WriteString("Starting a Gradle Daemon, 1 busy Daemon could not be reused, use --status for details\n")
	for i := 0; i < tasks; i++ {
		fmt.Fprintf(&b, "> Task :module%d:compileJava\n", i)
	}
	b.WriteString("BUILD SUCCESSFUL in 4s\n")
	fmt.Fprintf(&b, "%d actionable tasks: %d executed\n", tasks, tasks)
	return b.String()
}
