      *****************************************************************
      * The record this entry is about: a table one of whose items is
      * redefined, so the alternative is chosen once per occurrence
      * rather than once per record.
      *
      * A type code, a count, one to three address entries depending on
      * that count, and a four-byte item behind the table.
      *
      * Each entry carries a kind byte and a body. ADR-DOMESTIC is the
      * redefined item -- the first alternative, the one ADR-FOREIGN
      * names -- and the two are told apart by ADR-KIND, which sits
      * inside the entry the choice is being made for:
      *
      *   ADR-KIND      X(1)     offset 0 of the entry, 1 byte
      *   ADR-DOMESTIC           offset 1, 7 bytes
      *     ADR-STATE   X(2)     offset 1, 2 bytes
      *     ADR-ZIP     X(5)     offset 3, 5 bytes
      *   ADR-FOREIGN            offset 1, 7 bytes, redefining it
      *     ADR-COUNTRY X(3)     offset 1, 3 bytes
      *     ..slack..            offset 4, 4 bytes
      *
      * ADR-FOREIGN is four bytes shorter than the run it redefines,
      * which is ordinary COBOL: the entry is seven bytes wide whichever
      * alternative it carries, and in a foreign entry four of them
      * belong to no item at all. Those four travel with the occurrence
      * they were read from and are put back where they were found, and
      * an entry that takes the other alternative has none of them --
      * so a consumer that pairs the runs back up by counting them is
      * a consumer whose write-back depends on which arms were chosen.
      *
      * ADR-TAIL is what turns a width error into a visible one. It
      * begins at the byte after the last entry, so a consumer that
      * dropped a foreign entry's slack, or read the table at a
      * different number of occurrences than the reading states, reads
      * ADR-TAIL out of bytes belonging to something else.
      *****************************************************************
       01  ADR-RECORD.
           05  ADR-TYPE                PIC X(1).
           05  ADR-ENTRY-COUNT         PIC 9(1).
           05  ADR-ENTRY OCCURS 1 TO 3 TIMES
                       DEPENDING ON ADR-ENTRY-COUNT.
               10  ADR-KIND            PIC X(1).
               10  ADR-DOMESTIC.
                   15  ADR-STATE       PIC X(2).
                   15  ADR-ZIP         PIC X(5).
               10  ADR-FOREIGN REDEFINES ADR-DOMESTIC.
                   15  ADR-COUNTRY     PIC X(3).
           05  ADR-TAIL                PIC X(4).
