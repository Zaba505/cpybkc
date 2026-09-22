      *****************************************************************
      * The record this entry is about: a table whose entries carry
      * roles rather than types, so which alternative an entry holds is
      * decided by where the entry sits and by nothing in its bytes.
      *
      * A type code, a count, one to four address entries depending on
      * that count, and a four-byte item behind the table.
      *
      * ADR-HOME is the redefined item -- the first alternative, the one
      * the other two name -- and the three are told apart by nothing at
      * all, because no item of the record says which an entry carries:
      *
      *   ADR-HOME             offset 0 of the entry, 7 bytes
      *     ADR-STATE   X(2)   offset 0, 2 bytes
      *     ADR-ZIP     X(5)   offset 2, 5 bytes
      *   ADR-WORK             offset 0, 7 bytes, redefining it
      *     ADR-DESK    X(4)   offset 0, 4 bytes
      *     ..slack..          offset 4, 3 bytes
      *   ADR-MAIL             offset 0, 7 bytes, redefining it as well
      *     ADR-BOX     X(7)   offset 0, 7 bytes
      *
      * Entry one is the home address, entries two and three are work
      * addresses and entry four is the mailing address. That is the
      * whole of the rule; it is written in the layout and nowhere in
      * the file, and there is no kind byte for a predicate to test --
      * which is what makes this entry about a schedule rather than
      * about a discriminator.
      *
      * ADR-WORK is three bytes shorter than the run it redefines,
      * which is ordinary COBOL: the entry is seven bytes wide whichever
      * alternative it carries, and in a work entry three of them belong
      * to no item at all. Those three travel with the occurrence they
      * were read from and are put back where they were found. Which
      * occurrences have them is the descriptor's here rather than the
      * data's -- entries two and three, in every record of the file --
      * and that is the difference between a scheduled variant and a
      * byte-selected one, stated in bytes.
      *
      * ADR-TAIL is what turns a width error into a visible one. It
      * begins at the byte after the last entry, so a consumer that
      * dropped a work entry's slack, or read the table at a different
      * number of occurrences than the reading states, reads ADR-TAIL
      * out of bytes belonging to something else.
      *****************************************************************
       01  ADR-RECORD.
           05  ADR-TYPE                PIC X(1).
           05  ADR-ENTRY-COUNT         PIC 9(1).
           05  ADR-ENTRY OCCURS 1 TO 4 TIMES
                       DEPENDING ON ADR-ENTRY-COUNT.
               10  ADR-HOME.
                   15  ADR-STATE       PIC X(2).
                   15  ADR-ZIP         PIC X(5).
               10  ADR-WORK REDEFINES ADR-HOME.
                   15  ADR-DESK        PIC X(4).
               10  ADR-MAIL REDEFINES ADR-HOME.
                   15  ADR-BOX         PIC X(7).
           05  ADR-TAIL                PIC X(4).
