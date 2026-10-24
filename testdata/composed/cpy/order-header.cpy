000010*  An order's header record: the shared header, then the header's
000020*  own body.
000030 01  ORDER-HEADER-REC.
000040     COPY STDHDR.
000050     05  OH-CUSTOMER         PIC X(8).
000060     05  OH-DATE             PIC 9(8).
