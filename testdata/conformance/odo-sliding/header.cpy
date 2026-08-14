      *****************************************************************
      * The record that opens the odo-sliding file.
      *
      * Three bytes: the type code the layout discriminates on, and the
      * number of detail records that follow, as an unsigned zoned
      * count. The count governs records other than the one it sits in,
      * which is what makes it the automaton's register rather than an
      * item some repetition inside this record names.
      *
      * Nothing here repeats, and nothing here is packed, binary or
      * signed, so the bytes of the record are readable in a hex dump.
      *****************************************************************
       01  ODO-HEADER.
           05  HDR-TYPE                PIC X(1).
           05  HDR-DETAIL-COUNT        PIC 9(2).
