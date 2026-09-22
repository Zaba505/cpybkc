      *****************************************************************
      * The member record: a table of addresses whose entries carry
      * roles rather than types.
      *
      * A member, then MBR-ADDRESS-COUNT saying how many addresses the
      * member gave, and then that many of them. Behind the table sits
      * MBR-STATUS, so where it begins depends on how many addresses
      * arrived -- which is what an OCCURS DEPENDING ON reading decides,
      * and why member.sexpr has to state one.
      *
      * Inside one entry, ADR-HOME is described three ways: as itself,
      * as ADR-WORK and as ADR-MAIL. That REDEFINES is inside a group
      * that repeats, so the choice is made once per entry and not once
      * per record.
      *
      * What no byte of this copybook says is which of the three an
      * entry holds. There is no kind code here and there is no room for
      * one: the enrolment screen asks for a home address, then a work
      * address, then a mailing address, and writes them in that order.
      * Entry one is the home address because it is the first, entry two
      * the work address, entry three the mailing address. A member who
      * gave two addresses wrote a home and a work address, and a member
      * who gave one wrote a home address.
      *
      * So the rule is about position and the file carries no trace of
      * it. It lives in the layout, as a schedule-variant, and that is
      * the whole of what this example is here to show.
      *
      * ADR-MAIL describes thirty-four of the sixty-one bytes an entry
      * holds, which is ordinary COBOL and leaves twenty-seven bytes at
      * the end of every mailing address that no description covers. An
      * entry that read those bytes carries them back out unchanged --
      * docs/ir/SPEC.md's "Slack survives a read". Which entry has them
      * is the schedule's: entry three of every member with three
      * addresses, and no other entry of any member ever.
      *****************************************************************
       01  MEMBER-RECORD.
           05  MBR-MEMBER-ID           PIC X(10).
           05  MBR-NAME                PIC X(30).
           05  MBR-ADDRESS-COUNT       PIC 9(1).
           05  MBR-ADDRESS             OCCURS 1 TO 3 TIMES
                                       DEPENDING ON MBR-ADDRESS-COUNT.
               10  ADR-HOME.
                   15  AHM-STREET      PIC X(30).
                   15  AHM-CITY        PIC X(20).
                   15  AHM-REGION      PIC X(2).
                   15  AHM-POSTAL      PIC X(9).
               10  ADR-WORK REDEFINES ADR-HOME.
                   15  AWK-EMPLOYER    PIC X(30).
                   15  AWK-STREET      PIC X(20).
                   15  AWK-REGION      PIC X(2).
                   15  AWK-POSTAL      PIC X(9).
               10  ADR-MAIL REDEFINES ADR-HOME.
                   15  AML-BOX         PIC X(12).
                   15  AML-CITY        PIC X(20).
                   15  AML-REGION      PIC X(2).
           05  MBR-STATUS              PIC X(1).
