package harness

import "testing"

func TestCommentOnlyDiff(t *testing.T) {
	cases := []struct {
		name string
		diff string
		want bool
	}{
		{"whole-line comment removal", `diff --git a/F.java b/F.java
--- a/F.java
+++ b/F.java
@@ -1,3 +1,2 @@
 int x = 0;
-// increment the counter
 x++;`, true},
		{"javadoc block removal", `--- a/F.java
+++ b/F.java
@@ -1,6 +1,2 @@
-/**
- * Gets the name.
- * @return the name
- */
 public String getName() {`, true},
		{"code line touched", `--- a/F.java
+++ b/F.java
@@ -1,2 +1,1 @@
-int x = 0; // counter
+int x = 0;`, false},
		{"code deleted", `--- a/F.java
+++ b/F.java
@@ -1,2 +1,1 @@
-x++;
-// bump
 done();`, false},
		{"blank line cleanup with comments", `--- a/f.py
+++ b/f.py
@@ -1,4 +1,1 @@
-# what this does
-
 run()`, true},
		{"no changes at all", "diff --git a/F b/F", false},
		{"code added", `--- a/F.java
+++ b/F.java
@@ -1,1 +1,2 @@
 x++;
+y++;`, false},
	}
	for _, c := range cases {
		if got := commentOnlyDiff(c.diff); got != c.want {
			t.Errorf("%s: commentOnlyDiff = %v, want %v", c.name, got, c.want)
		}
	}
}
