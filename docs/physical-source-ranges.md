# Physical occurrence ranges

Before: EndLine used start + matched entry count - 1. Blank source lines are omitted by parsing, so a six-entry match beginning on line 2 incorrectly ended on line 7 even when the final entry was on line 10. Different occurrences could require different physical spans.

After: Each occurrence uses its own final parsed entry source line. LineCount remains the number of matched entries. TestRunPreservesEachOccurrencesPhysicalEndLine scans files with different blank-line spacing and verifies each returned range contains the complete sample. The regression failed on the original implementation and passes with the fix.
