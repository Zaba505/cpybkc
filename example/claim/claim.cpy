      *****************************************************************
      * The claim record: a table of line items, counted by a field in
      * front of it, and each line's body described three ways.
      *
      * The header is the claim itself -- what it is, which claim it is,
      * and whose. Behind it CLM-LINE-COUNT says how many line items
      * arrived, and CLM-LINE occurs that many times.
      *
      * Inside one line, CLN-PROFESSIONAL is described three ways: as
      * itself, as CLN-PHARMACY and as CLN-FACILITY. That REDEFINES is
      * inside a group that repeats, so the choice is made once per
      * line and not once per record: nine lines choosing between three
      * descriptions are not three record types, and a layout naming
      * this copybook says what selects each of them per occurrence.
      *
      * CLN-KIND opens the line and is what the selection reads. It sits
      * inside the occurrence, which is where an arm's target has to sit
      * -- a byte outside the table would choose the same description
      * for every line, and a choice made once per record belongs to the
      * record.
      *
      * CLN-PHARMACY is six bytes shorter than the run it redefines,
      * which is ordinary COBOL and leaves six bytes at the end of every
      * pharmacy line that no description covers. A line that read those
      * bytes carries them back out unchanged -- docs/ir/SPEC.md's
      * "Slack survives a read" -- and it carries them per occurrence,
      * so a claim of nine pharmacy lines holds nine such runs.
      *
      * CLM-TOTAL-CHARGE and CLM-STATUS sit behind the table, so where
      * they begin depends on how many lines the claim carried. Which
      * is the whole of what an OCCURS DEPENDING ON reading decides, and
      * why claim.sexpr has to state one.
      *****************************************************************
       01  CLAIM-RECORD.
           05  CLM-TYPE                PIC X(2).
           05  CLM-NUMBER              PIC X(12).
           05  CLM-MEMBER              PIC X(10).
           05  CLM-LINE-COUNT          PIC 9(1).
           05  CLM-LINE                OCCURS 1 TO 9 TIMES
                                       DEPENDING ON CLM-LINE-COUNT.
               10  CLN-KIND            PIC X(1).
               10  CLN-CHARGE          PIC S9(7)V99 COMP-3.
               10  CLN-PROFESSIONAL.
                   15  CPR-PROVIDER    PIC X(10).
                   15  CPR-PROCEDURE   PIC X(5).
                   15  CPR-MODIFIER    PIC X(2).
                   15  CPR-UNITS       PIC 9(3).
               10  CLN-PHARMACY        REDEFINES CLN-PROFESSIONAL.
                   15  CPH-NDC         PIC X(11).
                   15  CPH-DAYS-SUPPLY PIC 9(3).
               10  CLN-FACILITY        REDEFINES CLN-PROFESSIONAL.
                   15  CFA-FACILITY    PIC X(10).
                   15  CFA-REVENUE     PIC X(4).
                   15  CFA-BILL-TYPE   PIC X(3).
                   15  CFA-COVERED-DAYS PIC 9(3).
           05  CLM-TOTAL-CHARGE        PIC S9(9)V99 COMP-3.
           05  CLM-STATUS              PIC X(1).
