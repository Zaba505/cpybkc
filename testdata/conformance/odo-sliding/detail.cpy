      *****************************************************************
      * The record this entry is about: a table whose length is data.
      *
      * The type code, an unsigned zoned count of the occurrences that
      * follow it, a table of one to four three-byte occurrences
      * depending on that count, and a four-byte item behind the table.
      *
      * DTL-TAIL is what turns a width error into a visible one. Under
      * the sliding reading it begins at the byte after the last
      * occurrence the count states, so a consumer that read the table
      * at a fixed four occurrences, or at one occurrence too few, reads
      * DTL-TAIL out of bytes belonging to something else -- the same
      * trick packed-comp6 plays for a one-byte overread, here for a
      * variable one.
      *
      * The extent is therefore 6 + 3n bytes rather than a constant,
      * which is why the file this record sits in is framed.
      *****************************************************************
       01  ODO-DETAIL.
           05  DTL-TYPE                PIC X(1).
           05  DTL-LINE-COUNT          PIC 9(1).
           05  DTL-LINE OCCURS 1 TO 4 TIMES
                       DEPENDING ON DTL-LINE-COUNT.
               10  DTL-SKU             PIC X(3).
           05  DTL-TAIL                PIC X(4).
