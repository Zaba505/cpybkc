      *****************************************************************
      * The pair's copybook with the count taken out of it: the table is
      * a fixed OCCURS, so four entries is a fact of the copybook rather
      * than of how an OCCURS DEPENDING ON is read, and no item of the
      * record is a count.
      *
      * A type code, four address entries, and a four-byte item behind
      * the table.
      *
      * ADR-HOME is the redefined item and the layout schedules the
      * three alternatives over the four positions exactly as it does
      * for the pair. Nothing in an entry says which it carries:
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
      * addresses and entry four is the mailing address, and every
      * record of the file holds all four of them because the table's
      * length is not data.
      *
      * ADR-TAIL sits behind the table for the reason it does in the
      * pair: a consumer that dropped a work entry's slack run reads it
      * three bytes early, and under recfm F the record behind it moves
      * with it.
      *****************************************************************
       01  ADR-RECORD.
           05  ADR-TYPE                PIC X(1).
           05  ADR-ENTRY OCCURS 4 TIMES.
               10  ADR-HOME.
                   15  ADR-STATE       PIC X(2).
                   15  ADR-ZIP         PIC X(5).
               10  ADR-WORK REDEFINES ADR-HOME.
                   15  ADR-DESK        PIC X(4).
               10  ADR-MAIL REDEFINES ADR-HOME.
                   15  ADR-BOX         PIC X(7).
           05  ADR-TAIL                PIC X(4).
