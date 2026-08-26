      *****************************************************************
      * PXTRACT -- THE DAILY POLICY ADMINISTRATION EXTRACT.
      *
      * ONE MEMBER, ELEVEN 01-LEVELS: THE FILE HEADER, THE FILE
      * TRAILER, AND THE NINE DETAIL RECORD TYPES THE POLICY MASTER
      * UNLOADS. EVERY RECORD OPENS WITH ITS THREE-BYTE TYPE CODE, AND
      * EVERY DETAIL RECORD CARRIES THE POLICY KEY BEHIND IT SO THAT A
      * DOWNSTREAM SORT CAN GROUP THE FILE WITHOUT READING PAST BYTE
      * THIRTY-TWO.
      *
      * THE DATASET IS RECFM=FB, LRECL=256. EVERY RECORD TYPE
      * THEREFORE ACCOUNTS FOR ALL 256 BYTES, AND THE ONES WHOSE
      * FIELDS STOP SHORT CARRY THE REST AS FILLER -- WHICH IS WHY THE
      * SHORTER TYPES END IN A FILLER OF A HUNDRED BYTES OR MORE.
      *
      * FIELDS ARE REPEATED ACROSS RECORD TYPES RATHER THAN FACTORED
      * OUT. COBOL HAS NO INHERITANCE AND A COPY MEMBER SHARED BETWEEN
      * 01-LEVELS WOULD GIVE EVERY RECORD TYPE THE SAME DATA-NAMES, SO
      * EACH TYPE CARRIES THE KEY UNDER ITS OWN PREFIX. THAT IS WHY A
      * MERGED TABLE OF THIS FILE IS WIDER THAN THE FIELDS IT HOLDS.
      *****************************************************************

      *****************************************************************
      * 000 -- THE FILE HEADER. ONE PER EXTRACT, AND THE ONLY RECORD
      * WITH NO POLICY KEY: IT DESCRIBES THE RUN AND NOT A POLICY.
      *****************************************************************
       01  PX-FILE-HEADER.
           05  FHD-RECORD-TYPE         PIC X(3).
           05  FHD-EXTRACT-NAME        PIC X(20).
           05  FHD-CYCLE-DATE          PIC 9(8).
           05  FHD-CYCLE-TIME          PIC 9(6).
           05  FHD-CARRIER             PIC X(4).
           05  FHD-REGION              PIC X(2).
           05  FHD-SOURCE-SYSTEM       PIC X(8).
           05  FHD-RUN-NUMBER          PIC 9(4).
           05  FHD-FORMAT-VERSION      PIC 9(3).
           05  FILLER                  PIC X(198).

      *****************************************************************
      * PLC -- THE POLICY RECORD. ONE PER POLICY TERM, AND THE RECORD
      * EVERY DETAIL TYPE BELOW IT HANGS OFF.
      *****************************************************************
       01  PX-POLICY.
           05  PLC-RECORD-TYPE         PIC X(3).
           05  PLC-POLICY-NUMBER       PIC X(12).
           05  PLC-CARRIER             PIC X(4).
           05  PLC-TERM-EFF-DATE       PIC 9(8).
           05  PLC-SEQ                 PIC 9(5).
           05  PLC-PRODUCT-CODE        PIC X(6).
           05  PLC-LOB                 PIC X(4).
           05  PLC-STATE               PIC X(2).
           05  PLC-TERM-EXP-DATE       PIC 9(8).
           05  PLC-ISSUE-DATE          PIC 9(8).
           05  PLC-STATUS              PIC X(2).
           05  PLC-CANCEL-DATE         PIC 9(8).
           05  PLC-CANCEL-REASON       PIC X(3).
           05  PLC-AGENCY-CODE         PIC X(8).
           05  PLC-PRODUCER-CODE       PIC X(8).
           05  PLC-BILL-METHOD         PIC X(2).
           05  PLC-PAY-PLAN            PIC X(3).
           05  PLC-TERM-MONTHS         PIC 9(2).
           05  PLC-WRITTEN-PREMIUM     PIC S9(9)V99 COMP-3.
           05  PLC-RENEWAL-COUNT       PIC 9(3).
           05  FILLER                  PIC X(151).

      *****************************************************************
      * INS -- A NAMED INSURED. THE WIDEST DETAIL TYPE, BECAUSE IT
      * CARRIES A MAILING ADDRESS.
      *****************************************************************
       01  PX-INSURED.
           05  INS-RECORD-TYPE         PIC X(3).
           05  INS-POLICY-NUMBER       PIC X(12).
           05  INS-CARRIER             PIC X(4).
           05  INS-TERM-EFF-DATE       PIC 9(8).
           05  INS-SEQ                 PIC 9(5).
           05  INS-ROLE                PIC X(2).
           05  INS-LAST-NAME           PIC X(30).
           05  INS-FIRST-NAME          PIC X(20).
           05  INS-MIDDLE-INITIAL      PIC X(1).
           05  INS-ADDRESS-LINE-1      PIC X(35).
           05  INS-ADDRESS-LINE-2      PIC X(35).
           05  INS-CITY                PIC X(25).
           05  INS-STATE               PIC X(2).
           05  INS-POSTAL-CODE         PIC X(9).
           05  INS-COUNTRY             PIC X(3).
           05  INS-BIRTH-DATE          PIC 9(8).
           05  INS-GENDER              PIC X(1).
           05  INS-MARITAL-STATUS      PIC X(1).
           05  INS-TAX-ID-LAST-4       PIC X(4).
           05  INS-EMAIL-ON-FILE       PIC X(1).
           05  FILLER                  PIC X(47).

      *****************************************************************
      * LOC -- AN INSURED LOCATION. PROPERTY POLICIES CARRY MANY;
      * AUTOMOBILE POLICIES CARRY NONE, WHICH IS WHERE THE SPARSITY
      * COMES FROM.
      *****************************************************************
       01  PX-LOCATION.
           05  LOC-RECORD-TYPE         PIC X(3).
           05  LOC-POLICY-NUMBER       PIC X(12).
           05  LOC-CARRIER             PIC X(4).
           05  LOC-TERM-EFF-DATE       PIC 9(8).
           05  LOC-SEQ                 PIC 9(5).
           05  LOC-LOCATION-NUMBER     PIC 9(4).
           05  LOC-ADDRESS-LINE-1      PIC X(35).
           05  LOC-CITY                PIC X(25).
           05  LOC-STATE               PIC X(2).
           05  LOC-POSTAL-CODE         PIC X(9).
           05  LOC-COUNTY-CODE         PIC X(5).
           05  LOC-TERRITORY           PIC X(4).
           05  LOC-PROTECTION-CLASS    PIC X(2).
           05  LOC-CONSTRUCTION-CODE   PIC X(2).
           05  LOC-OCCUPANCY-CODE      PIC X(3).
           05  LOC-YEAR-BUILT          PIC 9(4).
           05  LOC-SQUARE-FEET         PIC 9(6).
           05  LOC-NUMBER-OF-STORIES   PIC 9(2).
           05  LOC-DISTANCE-TO-FIRE    PIC 9(3)V9.
           05  LOC-FLOOD-ZONE          PIC X(4).
           05  FILLER                  PIC X(113).

      *****************************************************************
      * VEH -- AN INSURED VEHICLE. AUTOMOBILE POLICIES CARRY MANY;
      * PROPERTY POLICIES CARRY NONE.
      *****************************************************************
       01  PX-VEHICLE.
           05  VEH-RECORD-TYPE         PIC X(3).
           05  VEH-POLICY-NUMBER       PIC X(12).
           05  VEH-CARRIER             PIC X(4).
           05  VEH-TERM-EFF-DATE       PIC 9(8).
           05  VEH-SEQ                 PIC 9(5).
           05  VEH-UNIT-NUMBER         PIC 9(4).
           05  VEH-VIN                 PIC X(17).
           05  VEH-MODEL-YEAR          PIC 9(4).
           05  VEH-MAKE                PIC X(15).
           05  VEH-MODEL               PIC X(20).
           05  VEH-BODY-STYLE          PIC X(4).
           05  VEH-VEHICLE-USE         PIC X(2).
           05  VEH-GARAGE-STATE        PIC X(2).
           05  VEH-GARAGE-POSTAL       PIC X(9).
           05  VEH-ANNUAL-MILEAGE      PIC 9(6).
           05  VEH-SYMBOL              PIC X(3).
           05  VEH-ANTI-THEFT          PIC X(1).
           05  VEH-COST-NEW            PIC 9(7).
           05  VEH-LIENHOLDER-NAME     PIC X(30).
           05  VEH-LEASED-FLAG         PIC X(1).
           05  FILLER                  PIC X(99).

      *****************************************************************
      * DRV -- A RATED OR EXCLUDED DRIVER.
      *****************************************************************
       01  PX-DRIVER.
           05  DRV-RECORD-TYPE         PIC X(3).
           05  DRV-POLICY-NUMBER       PIC X(12).
           05  DRV-CARRIER             PIC X(4).
           05  DRV-TERM-EFF-DATE       PIC 9(8).
           05  DRV-SEQ                 PIC 9(5).
           05  DRV-DRIVER-NUMBER       PIC 9(3).
           05  DRV-LAST-NAME           PIC X(30).
           05  DRV-FIRST-NAME          PIC X(20).
           05  DRV-BIRTH-DATE          PIC 9(8).
           05  DRV-GENDER              PIC X(1).
           05  DRV-MARITAL-STATUS      PIC X(1).
           05  DRV-LICENSE-STATE       PIC X(2).
           05  DRV-LICENSE-NUMBER      PIC X(20).
           05  DRV-LICENSE-DATE        PIC 9(8).
           05  DRV-RELATION-TO-INS     PIC X(2).
           05  DRV-RATED-UNIT          PIC 9(4).
           05  DRV-GOOD-STUDENT        PIC X(1).
           05  DRV-TRAINING-FLAG       PIC X(1).
           05  DRV-POINTS              PIC 9(2).
           05  DRV-EXCLUDED-FLAG       PIC X(1).
           05  FILLER                  PIC X(120).

      *****************************************************************
      * COV -- A COVERAGE, WHICH MAY HANG OFF A LOCATION, OFF A UNIT,
      * OR OFF THE POLICY ITSELF. THE TWO KEYS ARE ZERO WHERE THEY DO
      * NOT APPLY.
      *****************************************************************
       01  PX-COVERAGE.
           05  COV-RECORD-TYPE         PIC X(3).
           05  COV-POLICY-NUMBER       PIC X(12).
           05  COV-CARRIER             PIC X(4).
           05  COV-TERM-EFF-DATE       PIC 9(8).
           05  COV-SEQ                 PIC 9(5).
           05  COV-LOCATION-NUMBER     PIC 9(4).
           05  COV-UNIT-NUMBER         PIC 9(4).
           05  COV-COVERAGE-CODE       PIC X(6).
           05  COV-COVERAGE-DESC       PIC X(30).
           05  COV-LIMIT-1             PIC S9(11)V99 COMP-3.
           05  COV-LIMIT-2             PIC S9(11)V99 COMP-3.
           05  COV-DEDUCTIBLE          PIC S9(7)V99 COMP-3.
           05  COV-DEDUCTIBLE-TYPE     PIC X(2).
           05  COV-EFF-DATE            PIC 9(8).
           05  COV-EXP-DATE            PIC 9(8).
           05  COV-FORM-CODE           PIC X(8).
           05  COV-CLASS-CODE          PIC X(6).
           05  COV-RATE                PIC S9(5)V9(5) COMP-3.
           05  COV-EXPOSURE-BASIS      PIC X(2).
           05  COV-WAIVER-FLAG         PIC X(1).
           05  FILLER                  PIC X(120).

      *****************************************************************
      * PRM -- A PREMIUM TRANSACTION. THE ACCOUNTING GRAIN OF THE
      * FILE, AND THE ONE MOST OF ITS RECORDS ARE.
      *****************************************************************
       01  PX-PREMIUM.
           05  PRM-RECORD-TYPE         PIC X(3).
           05  PRM-POLICY-NUMBER       PIC X(12).
           05  PRM-CARRIER             PIC X(4).
           05  PRM-TERM-EFF-DATE       PIC 9(8).
           05  PRM-SEQ                 PIC 9(5).
           05  PRM-COVERAGE-CODE       PIC X(6).
           05  PRM-TRANSACTION-CODE    PIC X(4).
           05  PRM-TRANSACTION-DATE    PIC 9(8).
           05  PRM-ACCOUNTING-PERIOD   PIC 9(6).
           05  PRM-WRITTEN-AMOUNT      PIC S9(9)V99 COMP-3.
           05  PRM-EARNED-AMOUNT       PIC S9(9)V99 COMP-3.
           05  PRM-UNEARNED-AMOUNT     PIC S9(9)V99 COMP-3.
           05  PRM-COMMISSION-AMOUNT   PIC S9(7)V99 COMP-3.
           05  PRM-COMMISSION-RATE     PIC S9(3)V9(4) COMP-3.
           05  PRM-TAX-AMOUNT          PIC S9(7)V99 COMP-3.
           05  PRM-FEE-AMOUNT          PIC S9(7)V99 COMP-3.
           05  PRM-SURCHARGE-AMOUNT    PIC S9(7)V99 COMP-3.
           05  PRM-CURRENCY            PIC X(3).
           05  PRM-GL-ACCOUNT          PIC X(10).
           05  PRM-STATUTORY-LINE      PIC X(4).
           05  FILLER                  PIC X(141).

      *****************************************************************
      * CLM -- AN OPEN OR CLOSED CLAIM. FEW POLICIES CARRY ONE, WHICH
      * IS THE OTHER HALF OF WHERE THE SPARSITY COMES FROM.
      *****************************************************************
       01  PX-CLAIM.
           05  CLM-RECORD-TYPE         PIC X(3).
           05  CLM-POLICY-NUMBER       PIC X(12).
           05  CLM-CARRIER             PIC X(4).
           05  CLM-TERM-EFF-DATE       PIC 9(8).
           05  CLM-SEQ                 PIC 9(5).
           05  CLM-CLAIM-NUMBER        PIC X(14).
           05  CLM-LOSS-DATE           PIC 9(8).
           05  CLM-REPORTED-DATE       PIC 9(8).
           05  CLM-CLOSED-DATE         PIC 9(8).
           05  CLM-STATUS              PIC X(2).
           05  CLM-CAUSE-OF-LOSS       PIC X(4).
           05  CLM-COVERAGE-CODE       PIC X(6).
           05  CLM-LOCATION-NUMBER     PIC 9(4).
           05  CLM-UNIT-NUMBER         PIC 9(4).
           05  CLM-ADJUSTER-CODE       PIC X(8).
           05  CLM-PAID-INDEMNITY      PIC S9(11)V99 COMP-3.
           05  CLM-PAID-EXPENSE        PIC S9(9)V99 COMP-3.
           05  CLM-RESERVE-INDEMNITY   PIC S9(11)V99 COMP-3.
           05  CLM-RESERVE-EXPENSE     PIC S9(9)V99 COMP-3.
           05  CLM-SUBROGATION-FLAG    PIC X(1).
           05  FILLER                  PIC X(131).

      *****************************************************************
      * ENR -- AN ENDORSEMENT FORM ATTACHED TO THE POLICY.
      *****************************************************************
       01  PX-ENDORSEMENT.
           05  ENR-RECORD-TYPE         PIC X(3).
           05  ENR-POLICY-NUMBER       PIC X(12).
           05  ENR-CARRIER             PIC X(4).
           05  ENR-TERM-EFF-DATE       PIC 9(8).
           05  ENR-SEQ                 PIC 9(5).
           05  ENR-ENDORSEMENT-NUMBER  PIC 9(4).
           05  ENR-FORM-NUMBER         PIC X(12).
           05  ENR-FORM-EDITION        PIC X(6).
           05  ENR-FORM-TITLE          PIC X(40).
           05  ENR-EFF-DATE            PIC 9(8).
           05  ENR-EXP-DATE            PIC 9(8).
           05  ENR-TRANSACTION-CODE    PIC X(4).
           05  ENR-TRANSACTION-DATE    PIC 9(8).
           05  ENR-PREMIUM-DELTA       PIC S9(9)V99 COMP-3.
           05  ENR-LOCATION-NUMBER     PIC 9(4).
           05  ENR-UNIT-NUMBER         PIC 9(4).
           05  ENR-COVERAGE-CODE       PIC X(6).
           05  ENR-MANDATORY-FLAG      PIC X(1).
           05  ENR-PRINT-FLAG          PIC X(1).
           05  ENR-STATE-FILED         PIC X(2).
           05  FILLER                  PIC X(110).

      *****************************************************************
      * 999 -- THE FILE TRAILER. THE CONTROL TOTALS THE RECEIVING
      * SYSTEM BALANCES THE EXTRACT ON.
      *****************************************************************
       01  PX-FILE-TRAILER.
           05  FTR-RECORD-TYPE         PIC X(3).
           05  FTR-CYCLE-DATE          PIC 9(8).
           05  FTR-POLICY-COUNT        PIC 9(9).
           05  FTR-DETAIL-COUNT        PIC 9(9).
           05  FTR-WRITTEN-PREMIUM     PIC S9(13)V99 COMP-3.
           05  FTR-PAID-LOSS           PIC S9(13)V99 COMP-3.
           05  FTR-HASH-POLICY-NUMBER  PIC 9(15).
           05  FTR-RUN-NUMBER          PIC 9(4).
           05  FILLER                  PIC X(192).
