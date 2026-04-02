# Kyma Adapter Stub — Scenario File Specification

Version: 1
Status: Design Document

---

## Overview

A **scenario** is a JSON file that defines one or more adapter stubs. Scenarios are loaded at startup and can be exported or imported without modification. At deploy time the runtime generates identifiers, status, and ingress URLs — none of those appear in the scenario file.

---

## Top-Level Structure

```json
{
  "version": 1,
  "name": "string — human-readable label for the scenario",
  "description": "string — optional, explains the integration being stubbed",
  "adapters": [ /* array of Adapter objects, see below */ ]
}
```

### Field Descriptions

| Field | Type | Required | Description |
|---|---|---|---|
| `version` | integer | yes | Schema version. Currently `1`. Used to detect and migrate older files. |
| `name` | string | yes | Short label shown in the UI and used as the Kubernetes resource prefix. Must be URL-safe (lowercase letters, numbers, hyphens). |
| `description` | string | no | Free-text explanation of what this scenario simulates. |
| `adapters` | array | yes | One or more adapter stub definitions. Order has no significance. |

---

## Adapter Object

Each entry in `adapters` describes one stub endpoint.

```json
{
  "name": "string — unique within the scenario",
  "type": "string — adapter type identifier",
  "description": "string — optional",
  "auth": { /* Auth block */ },
  "config": { /* Adapter-type-specific config */ }
}
```

### Field Descriptions

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Unique identifier within the scenario. Used as the Kubernetes Deployment/Service name suffix. Lowercase, hyphens only. |
| `type` | string | yes | One of: `rest`, `soap`, `xi`, `idoc`, `odata`, `sftp`, `as2`, `as4`. |
| `description` | string | no | What this specific adapter stub simulates. |
| `auth` | object | yes | Authentication block. Use `{ "type": "none" }` to disable auth. |
| `config` | object | yes | Adapter-type-specific configuration. Schema varies by `type`. |

---

## Auth Block

Auth is a typed block. The `type` field determines the shape of `config`.

```json
{
  "type": "none | basic | api_key | oauth2 | password",
  "config": { /* type-specific fields */ }
}
```

### `none` — No authentication

```json
{ "type": "none" }
```

No `config` needed. The stub accepts all requests.

### `basic` — HTTP Basic Authentication

```json
{
  "type": "basic",
  "config": {
    "username": "cpi_user",
    "password": "s3cr3t"
  }
}
```

The stub validates the `Authorization: Basic ...` header. Returns `401` if missing or wrong.

### `api_key` — API Key

```json
{
  "type": "api_key",
  "config": {
    "header": "X-API-Key",
    "value": "abc123def456"
  }
}
```

| Field | Description |
|---|---|
| `header` | HTTP header name the key is expected in. |
| `value` | The expected key value. |

### `oauth2` — Mock OAuth2 Token

```json
{
  "type": "oauth2",
  "config": {
    "token_endpoint": "/oauth/token",
    "client_id": "cpi-client",
    "client_secret": "top-secret",
    "token_ttl_seconds": 3600
  }
}
```

The stub exposes a `/oauth/token` endpoint that issues a synthetic Bearer token. Requests to the adapter must present a valid Bearer token in the `Authorization` header. No actual OAuth2 provider is involved. `token_endpoint` is relative to the adapter's own ingress URL.

### `password` — SFTP Password (SFTP adapters only)

```json
{
  "type": "password",
  "config": {
    "username": "cpiuser",
    "password": "cpipass"
  }
}
```

Used exclusively with the `sftp` adapter type. The SSH server validates these credentials.

---

## Adapter Type: `rest`

Simulates a generic HTTP REST endpoint. Returns a fixed response for any path under the adapter's base URL.

### Config Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `status_code` | integer | yes | HTTP status code returned for all requests. |
| `response_body` | string | yes | Response body. Can be JSON, XML, plain text, or empty string. |
| `response_headers` | object | no | Additional HTTP headers to include in every response. Keys and values are strings. |
| `response_delay_ms` | integer | no | Artificial delay in milliseconds before responding. Useful for timeout testing. Default: `0`. |

### Example — SAP S/4HANA Business Partner API stub

Simulates the SAP S/4HANA Business Partner API returning a single partner record.

```json
{
  "version": 1,
  "name": "s4-businesspartner-stub",
  "description": "Stubs the S/4HANA Business Partner API for CPI integration testing",
  "adapters": [
    {
      "name": "businesspartner-api",
      "type": "rest",
      "description": "Returns a fixed Business Partner response for GET /A_BusinessPartner",
      "auth": {
        "type": "basic",
        "config": {
          "username": "CPIADMIN",
          "password": "Welcome1"
        }
      },
      "config": {
        "status_code": 200,
        "response_headers": {
          "Content-Type": "application/json",
          "sap-metadata-last-modified": "2024-01-15T10:00:00Z"
        },
        "response_body": "{\"d\":{\"results\":[{\"BusinessPartner\":\"1000001\",\"BusinessPartnerFullName\":\"Acme Corporation\",\"BusinessPartnerIsBlocked\":false,\"BusinessPartnerType\":\"1\",\"Industry\":\"HT\",\"CountryRegion\":\"DE\",\"Language\":\"EN\"}]}}",
        "response_delay_ms": 0
      }
    }
  ]
}
```

---

## Adapter Type: `soap`

Simulates a SOAP 1.1 or 1.2 HTTP endpoint. Validates that incoming requests contain a SOAP envelope and returns a fixed SOAP response. Optionally echoes back WS-Security headers from the request.

### Config Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `soap_version` | string | yes | `"1.1"` or `"1.2"`. Determines the namespace and `Content-Type` used in responses. |
| `response_body` | string | yes | The full SOAP response envelope as an XML string. |
| `wssecurity_passthrough` | boolean | no | If `true`, the stub copies the `wsse:Security` header from the request into the response. Default: `false`. |
| `fault_on_missing_action` | boolean | no | If `true`, return a SOAP Fault if the `SOAPAction` header (1.1) or `action` parameter (1.2) is absent. Default: `false`. |
| `response_delay_ms` | integer | no | Artificial delay in milliseconds. Default: `0`. |

### Example — SAP ECC FI document posting service

Simulates a SOAP 1.1 service that accepts FI document posts and acknowledges them.

```json
{
  "version": 1,
  "name": "ecc-fi-posting-stub",
  "description": "Stubs the ECC FI document posting SOAP service",
  "adapters": [
    {
      "name": "fi-document-post",
      "type": "soap",
      "description": "Accepts BAPI_ACC_DOCUMENT_POST calls and returns success response",
      "auth": {
        "type": "basic",
        "config": {
          "username": "RFC_USER",
          "password": "Rfcpass01"
        }
      },
      "config": {
        "soap_version": "1.1",
        "wssecurity_passthrough": false,
        "fault_on_missing_action": true,
        "response_delay_ms": 120,
        "response_body": "<?xml version=\"1.0\" encoding=\"UTF-8\"?><soap:Envelope xmlns:soap=\"http://schemas.xmlsoap.org/soap/envelope/\"><soap:Header/><soap:Body><n0:BAPI_ACC_DOCUMENT_POSTResponse xmlns:n0=\"urn:sap-com:document:sap:rfc:functions\"><RETURN><item><TYPE>S</TYPE><ID>RW</ID><NUMBER>026</NUMBER><MESSAGE>Document 100000001 was posted in company code 1000</MESSAGE><LOG_NO/><LOG_MSG_NO>000000</LOG_MSG_NO><MESSAGE_V1>100000001</MESSAGE_V1><MESSAGE_V2>1000</MESSAGE_V2></item></RETURN></n0:BAPI_ACC_DOCUMENT_POSTResponse></soap:Body></soap:Envelope>"
      }
    }
  ]
}
```

---

## Adapter Type: `xi`

Extends the SOAP adapter with SAP Process Integration (PI/XI) specific headers. Validates XI headers on inbound requests and includes them in the response envelope.

### Config Fields

All SOAP config fields apply, plus:

| Field | Type | Required | Description |
|---|---|---|---|
| `soap_version` | string | yes | `"1.1"` only — XI uses SOAP 1.1. |
| `quality_of_service` | string | yes | XI QoS value. Typically `"ExactlyOnce"` or `"BestEffort"`. Validated on inbound requests. |
| `interface_name` | string | yes | The XI interface name expected in the `SAP_Interface` SOAP header. |
| `interface_namespace` | string | yes | The XI interface namespace expected in the `SAP_InterfaceNamespace` header. |
| `response_body` | string | yes | The full SOAP response envelope. |
| `validate_xi_headers` | boolean | no | If `true`, return a SOAP Fault when required XI headers are missing. Default: `true`. |
| `response_delay_ms` | integer | no | Artificial delay in milliseconds. Default: `0`. |

### Example — PI/XI Purchase Order confirmation

Simulates the XI receiver adapter confirming a Purchase Order creation message.

```json
{
  "version": 1,
  "name": "xi-po-confirmation-stub",
  "description": "Stubs the PI/XI receiver adapter for Purchase Order confirmations",
  "adapters": [
    {
      "name": "po-confirmation-xi",
      "type": "xi",
      "description": "Receives PurchaseOrderCreated XI messages and returns acknowledgement",
      "auth": {
        "type": "basic",
        "config": {
          "username": "XI_SENDER",
          "password": "Xi$end3r"
        }
      },
      "config": {
        "soap_version": "1.1",
        "quality_of_service": "ExactlyOnce",
        "interface_name": "PurchaseOrderConfirmation_In",
        "interface_namespace": "urn:acme.com:purchasing:po",
        "validate_xi_headers": true,
        "response_delay_ms": 200,
        "response_body": "<?xml version=\"1.0\" encoding=\"UTF-8\"?><soap:Envelope xmlns:soap=\"http://schemas.xmlsoap.org/soap/envelope/\" xmlns:sap=\"urn:sap.com:xi:XI:Message30\"><soap:Header><sap:Main versionMajor=\"003\" versionMinor=\"1\"><sap:MessageClass>ApplicationMessage</sap:MessageClass><sap:ProcessingMode>synchronous</sap:ProcessingMode><sap:MessageId>STUB-CONFIRM-001</sap:MessageId></sap:Main></soap:Header><soap:Body><po:PurchaseOrderConfirmationResponse xmlns:po=\"urn:acme.com:purchasing:po\"><Acknowledgement><Status>SUCCESS</Status><PurchaseOrderNumber>4500001234</PurchaseOrderNumber><ConfirmationTimestamp>2024-01-15T14:30:00Z</ConfirmationTimestamp></Acknowledgement></po:PurchaseOrderConfirmationResponse></soap:Body></soap:Envelope>"
      }
    }
  ]
}
```

---

## Adapter Type: `idoc`

Simulates the SAP IDoc SOAP endpoint. Accepts SOAP messages containing an IDoc XML payload and returns the standard `IDOC_INBOUND_ASYNCHRONOUS` acknowledgement structure.

### Config Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `idoc_type` | string | yes | Expected IDoc type (e.g., `ORDERS05`, `DESADV01`). Logged and optionally validated. |
| `validate_idoc_type` | boolean | no | If `true`, return a SOAP Fault when the IDoc type in the payload does not match `idoc_type`. Default: `false`. |
| `response_tid` | string | yes | Transaction ID returned in the acknowledgement. Can be a static UUID for testing. |
| `response_delay_ms` | integer | no | Artificial delay in milliseconds. Default: `0`. |

### Example — Sales Order IDoc receiver

Simulates an SAP system receiving ORDERS05 IDocs from CPI.

```json
{
  "version": 1,
  "name": "erp-orders-idoc-stub",
  "description": "Stubs the ERP system IDoc receiver for ORDERS05 sales order IDocs",
  "adapters": [
    {
      "name": "idoc-orders05-receiver",
      "type": "idoc",
      "description": "Accepts ORDERS05 IDocs and returns standard TID acknowledgement",
      "auth": {
        "type": "basic",
        "config": {
          "username": "IDOC_INBOUND",
          "password": "Idoc1nbound!"
        }
      },
      "config": {
        "idoc_type": "ORDERS05",
        "validate_idoc_type": true,
        "response_tid": "550e8400-e29b-41d4-a716-446655440000",
        "response_delay_ms": 50
      }
    }
  ]
}
```

---

## Adapter Type: `odata`

Simulates an OData v2 or v4 service. Serves a `$metadata` document and returns fixed entity set responses. Read-only — POST/PUT/PATCH/DELETE return `405 Method Not Allowed` unless overridden.

### Config Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `odata_version` | string | yes | `"v2"` or `"v4"`. Determines namespace URIs and response format in `$metadata`. |
| `service_path` | string | yes | The root path of the OData service (e.g., `/sap/opu/odata/sap/API_SALES_ORDER_SRV`). |
| `namespace` | string | yes | OData namespace used in the `$metadata` document (e.g., `SALESORDER_SRV`). |
| `entity_sets` | array | yes | List of entity set definitions. See Entity Set Object below. |
| `allow_write` | boolean | no | If `true`, POST/PUT/PATCH/DELETE return `201`/`200`/`204` with the submitted body echoed back. Default: `false`. |
| `response_delay_ms` | integer | no | Artificial delay in milliseconds. Default: `0`. |

### Entity Set Object

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Entity set name as it appears in the URL (e.g., `A_SalesOrder`). |
| `entity_type` | string | yes | Entity type name used in `$metadata` (e.g., `A_SalesOrderType`). |
| `key_property` | string | yes | Primary key property name (e.g., `SalesOrder`). |
| `response_body` | string | yes | JSON string containing the response for a collection request (`GET /EntitySet`). Individual key lookups return the first entity. |

### Example — SAP Sales Order OData service

Simulates the S/4HANA Sales Order API for CPI retrieval flows.

```json
{
  "version": 1,
  "name": "s4-salesorder-odata-stub",
  "description": "Stubs the S/4HANA Sales Order OData v2 API",
  "adapters": [
    {
      "name": "salesorder-srv",
      "type": "odata",
      "description": "Serves Sales Order and Sales Order Item entity sets",
      "auth": {
        "type": "oauth2",
        "config": {
          "token_endpoint": "/oauth/token",
          "client_id": "cpi-integration",
          "client_secret": "0auth2S3cr3t",
          "token_ttl_seconds": 3600
        }
      },
      "config": {
        "odata_version": "v2",
        "service_path": "/sap/opu/odata/sap/API_SALES_ORDER_SRV",
        "namespace": "API_SALES_ORDER_SRV",
        "allow_write": false,
        "response_delay_ms": 80,
        "entity_sets": [
          {
            "name": "A_SalesOrder",
            "entity_type": "A_SalesOrderType",
            "key_property": "SalesOrder",
            "response_body": "{\"d\":{\"results\":[{\"SalesOrder\":\"1000000001\",\"SalesOrderType\":\"OR\",\"SoldToParty\":\"1000001\",\"SalesOrganization\":\"1000\",\"DistributionChannel\":\"10\",\"Division\":\"00\",\"TotalNetAmount\":\"15000.00\",\"TransactionCurrency\":\"EUR\",\"CreationDate\":\"\\/Date(1705276800000)\\/\"},{\"SalesOrder\":\"1000000002\",\"SalesOrderType\":\"OR\",\"SoldToParty\":\"1000002\",\"SalesOrganization\":\"1000\",\"DistributionChannel\":\"10\",\"Division\":\"00\",\"TotalNetAmount\":\"8750.50\",\"TransactionCurrency\":\"USD\",\"CreationDate\":\"\\/Date(1705363200000)\\/\"}]}}"
          },
          {
            "name": "A_SalesOrderItem",
            "entity_type": "A_SalesOrderItemType",
            "key_property": "SalesOrderItem",
            "response_body": "{\"d\":{\"results\":[{\"SalesOrder\":\"1000000001\",\"SalesOrderItem\":\"10\",\"Material\":\"MAT-001\",\"SalesOrderItemText\":\"Widget A\",\"RequestedQuantity\":\"100\",\"RequestedQuantityUnit\":\"EA\",\"NetAmount\":\"10000.00\",\"TransactionCurrency\":\"EUR\"},{\"SalesOrder\":\"1000000001\",\"SalesOrderItem\":\"20\",\"Material\":\"MAT-002\",\"SalesOrderItemText\":\"Widget B\",\"RequestedQuantity\":\"50\",\"RequestedQuantityUnit\":\"EA\",\"NetAmount\":\"5000.00\",\"TransactionCurrency\":\"EUR\"}]}}"
          }
        ]
      }
    }
  ]
}
```

---

## Adapter Type: `sftp`

Runs an embedded SSH/SFTP server. CPI connects and transfers files using standard SFTP protocol. Files listed in `config.files` are pre-populated in the server's virtual filesystem at startup. Files uploaded by CPI are accepted and discarded (not persisted across restarts).

### Config Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `port` | integer | yes | SSH port the server listens on inside the pod. Typically `22`. The Kubernetes Service maps this to an external port. |
| `root_dir` | string | yes | Virtual filesystem root presented to the SFTP client (e.g., `/upload`). |
| `files` | array | no | Files pre-populated in the virtual filesystem at startup. See File Object below. |

### File Object

| Field | Type | Required | Description |
|---|---|---|---|
| `path` | string | yes | Path relative to `root_dir` (e.g., `outbound/invoice_001.xml`). |
| `content` | string | yes | File content. Plain text for XML/EDI. Base64-encoded for binary. |
| `encoding` | string | no | `"utf8"` (default) or `"base64"`. |

### Example — SFTP outbound for delivery notifications

Simulates an external logistics provider SFTP server where CPI deposits delivery notification XML files.

```json
{
  "version": 1,
  "name": "logistics-sftp-stub",
  "description": "Stubs the logistics provider SFTP drop zone for delivery notifications",
  "adapters": [
    {
      "name": "logistics-sftp",
      "type": "sftp",
      "description": "Accepts delivery notification files from CPI in /upload/outbound",
      "auth": {
        "type": "password",
        "config": {
          "username": "cpi_sftp",
          "password": "Sftp$ecure1"
        }
      },
      "config": {
        "port": 22,
        "root_dir": "/upload",
        "files": [
          {
            "path": "outbound/SAMPLE_DESADV.xml",
            "encoding": "utf8",
            "content": "<?xml version=\"1.0\" encoding=\"UTF-8\"?><DeliveryNotification><DocumentNumber>700000001</DocumentNumber><DeliveryDate>2024-01-20</DeliveryDate><ShipToParty>2000001</ShipToParty><Items><Item><ItemNumber>10</ItemNumber><Material>MAT-001</Material><DeliveredQuantity>100</DeliveredQuantity><UnitOfMeasure>EA</UnitOfMeasure></Item></Items></DeliveryNotification>"
          }
        ]
      }
    }
  ]
}
```

---

## Adapter Type: `as2`

Simulates an AS2 HTTP endpoint. Accepts MIME multipart AS2 messages and returns a synchronous MDN (Message Disposition Notification). No message signing or encryption is performed — the stub works in plaintext mode only.

### Config Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `as2_id` | string | yes | The AS2 ID of this stub (the receiver). Validated against the `AS2-To` header. |
| `partner_as2_id` | string | yes | The AS2 ID of the sender (CPI). Validated against the `AS2-From` header. |
| `mdn_mode` | string | yes | `"sync"` — synchronous MDN in the same HTTP response. (`"async"` not supported in v1.) |
| `mdn_disposition` | string | no | `"processed"` (default) or `"failed"`. Controls whether the MDN reports success or failure. |
| `response_delay_ms` | integer | no | Artificial delay in milliseconds. Default: `0`. |

### Example — EDI trading partner AS2 endpoint (ANSI X12 850 Purchase Order)

Simulates a supplier's AS2 endpoint receiving ANSI X12 850 Purchase Order transactions from CPI.

```json
{
  "version": 1,
  "name": "supplier-as2-x12-stub",
  "description": "Stubs the supplier AS2 endpoint receiving X12 850 Purchase Orders",
  "adapters": [
    {
      "name": "supplier-as2",
      "type": "as2",
      "description": "Receives X12 850 EDI messages and returns a positive synchronous MDN",
      "auth": {
        "type": "none"
      },
      "config": {
        "as2_id": "SUPPLIER-AS2-ID",
        "partner_as2_id": "ACME-CPI-AS2-ID",
        "mdn_mode": "sync",
        "mdn_disposition": "processed",
        "response_delay_ms": 300
      }
    }
  ]
}
```

**Note on ANSI X12 payload:** The actual X12 EDI content is carried inside the MIME body part by CPI. The stub does not parse it — it accepts and acknowledges any payload. A representative X12 850 looks like this (sent by CPI, not configured in the stub):

```
ISA*00*          *00*          *ZZ*ACMECORP        *ZZ*SUPPLIER       *240115*1400*^*00501*000000001*0*P*>~
GS*PO*ACMECORP*SUPPLIER*20240115*1400*1*X*005010~
ST*850*0001~
BEG*00*SA*PO-20240115-001**20240115~
REF*DP*DEPT-42~
DTM*002*20240120~
N1*BY*Acme Corporation*ZZ*ACMECORP~
N1*ST*Acme Warehouse Berlin*ZZ*WH-BERLIN~
PO1*1*100*EA*15.00**VP*SKU-001~
PID*F****Widget Model A~
PO1*2*50*EA*28.00**VP*SKU-002~
PID*F****Widget Model B~
CTT*2~
SE*14*0001~
GE*1*1~
IEA*1*000000001~
```

---

## Adapter Type: `as4`

Simulates an AS4 (ebMS3) HTTP endpoint. AS4 is a profile of ebMS3 and uses SOAP with specific ebMS3 messaging headers. The stub accepts AS4 SOAP messages and returns a valid `eb:SignalMessage` receipt. No signing or encryption is supported in v1.

### Config Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `party_id` | string | yes | The ebMS3 PartyId of this stub (the receiver). Validated against `eb:To/eb:PartyId`. |
| `partner_party_id` | string | yes | The ebMS3 PartyId of the sender (CPI). Validated against `eb:From/eb:PartyId`. |
| `service` | string | yes | The ebMS3 `eb:Service` value expected in inbound messages. |
| `action` | string | yes | The ebMS3 `eb:Action` value expected in inbound messages. |
| `mep` | string | no | Message Exchange Pattern. `"one-way"` (default) or `"two-way"`. In `"one-way"` mode the stub returns only a receipt Signal. |
| `validate_ebms_headers` | boolean | no | If `true`, return an ebMS3 Error signal when required headers are missing. Default: `true`. |
| `response_delay_ms` | integer | no | Artificial delay in milliseconds. Default: `0`. |

### Example — Peppol-style AS4 invoice submission

Simulates a Peppol Access Point receiving BIS Billing 3.0 invoice messages from CPI via AS4.

```json
{
  "version": 1,
  "name": "peppol-ap-as4-stub",
  "description": "Stubs a Peppol Access Point receiving UBL invoice messages over AS4",
  "adapters": [
    {
      "name": "peppol-as4-receiver",
      "type": "as4",
      "description": "Accepts ebMS3 AS4 messages containing UBL 2.1 invoices and returns a receipt",
      "auth": {
        "type": "basic",
        "config": {
          "username": "as4_sender",
          "password": "As4$end3r!"
        }
      },
      "config": {
        "party_id": "urn:peppol:participant:0088:stub-receiver",
        "partner_party_id": "urn:peppol:participant:0088:acme-sender",
        "service": "urn:fdc:peppol.eu:2017:poacc:billing:01:1.0",
        "action": "busdox-docid-qns::urn:oasis:names:specification:ubl:schema:xsd:Invoice-2::Invoice##urn:cen.eu:en16931:2017#compliant#urn:fdc:peppol.eu:2017:poacc:billing:3.0::2.1",
        "mep": "one-way",
        "validate_ebms_headers": true,
        "response_delay_ms": 150
      }
    }
  ]
}
```

---

## EDIFACT over SFTP

EDIFACT messages are files. There is no dedicated `edifact` adapter type. Configure an `sftp` adapter and pre-populate it with EDIFACT interchange files. CPI picks them up using the SFTP adapter.

### Example — EDIFACT ORDERS received via SFTP

Simulates a supplier SFTP server that has deposited EDIFACT ORDERS messages for CPI to collect.

```json
{
  "version": 1,
  "name": "supplier-edifact-sftp-stub",
  "description": "Stubs a supplier SFTP server with EDIFACT ORDERS files ready for CPI pickup",
  "adapters": [
    {
      "name": "edifact-sftp",
      "type": "sftp",
      "description": "Pre-populated with EDIFACT D96A ORDERS and ORDRSP test interchanges",
      "auth": {
        "type": "password",
        "config": {
          "username": "edi_user",
          "password": "Edi$ftp2024"
        }
      },
      "config": {
        "port": 22,
        "root_dir": "/edi",
        "files": [
          {
            "path": "inbound/ORDERS_20240115_001.edi",
            "encoding": "utf8",
            "content": "UNA:+.? 'UNB+UNOA:1+SUPPLIER:1+ACMECORP:1+240115:1400+000000001'UNH+1+ORDERS:D:96A:UN'BGM+220+PO-20240115-001+9'DTM+137:20240115:102'DTM+2:20240120:102'NAD+BY+ACMECORP::91++Acme Corporation+Hauptstrasse 1+Berlin++10115+DE'NAD+SU+SUPPLIER::91++Supplier GmbH+Lieferweg 5+Hamburg++20095+DE'LIN+1++SKU-001:SA'QTY+21:100:EA'PRI+AAA:15.00:CA'LIN+2++SKU-002:SA'QTY+21:50:EA'PRI+AAA:28.00:CA'UNS+S'CNT+2:2'UNT+16+1'UNZ+1+000000001'"
          },
          {
            "path": "inbound/ORDRSP_20240115_001.edi",
            "encoding": "utf8",
            "content": "UNA:+.? 'UNB+UNOA:1+SUPPLIER:1+ACMECORP:1+240115:1500+000000002'UNH+1+ORDRSP:D:96A:UN'BGM+231+RS-20240115-001+9'DTM+137:20240115:102'RFF+ON:PO-20240115-001'NAD+BY+ACMECORP::91++Acme Corporation'NAD+SU+SUPPLIER::91++Supplier GmbH'LIN+1++SKU-001:SA'QTY+21:100:EA'QTY+12:100:EA'PRI+AAA:15.00:CA'LIN+2++SKU-002:SA'QTY+21:50:EA'QTY+12:50:EA'PRI+AAA:28.00:CA'UNS+S'CNT+2:2'UNT+16+1'UNZ+1+000000002'"
          }
        ]
      }
    }
  ]
}
```

---

## ANSI X12 over AS2

ANSI X12 EDI can also be transmitted over AS2. Configure an `as2` adapter. The X12 interchange is delivered by CPI as the MIME body — the stub does not parse it, only acknowledges it.

See the [AS2 example](#adapter-type-as2) above, which already demonstrates X12 850 over AS2.

### Example — X12 810 Invoice acknowledgement via AS2

Simulates a retailer's AS2 endpoint receiving X12 810 Invoice transactions from a supplier via CPI.

```json
{
  "version": 1,
  "name": "retailer-as2-810-stub",
  "description": "Stubs the retailer AS2 endpoint receiving X12 810 Invoice EDI",
  "adapters": [
    {
      "name": "retailer-as2",
      "type": "as2",
      "description": "Receives X12 810 invoices from CPI and acknowledges with a synchronous MDN",
      "auth": {
        "type": "none"
      },
      "config": {
        "as2_id": "RETAILER-AS2",
        "partner_as2_id": "SUPPLIER-CPI-AS2",
        "mdn_mode": "sync",
        "mdn_disposition": "processed",
        "response_delay_ms": 200
      }
    }
  ]
}
```

A representative X12 810 Invoice (carried in the MIME body by CPI):

```
ISA*00*          *00*          *ZZ*SUPPLIER       *ZZ*RETAILER       *240115*1430*^*00501*000000002*0*P*>~
GS*IN*SUPPLIER*RETAILER*20240115*1430*2*X*005010~
ST*810*0001~
BIG*20240115*INV-20240115-001**PO-20240115-001~
REF*DP*DEPT-42~
DTM*011*20240115~
DTM*012*20240215~
N1*BT*Retailer Corp*ZZ*RETAILER~
N1*ST*Retailer Warehouse*ZZ*RW-001~
N1*SE*Supplier GmbH*ZZ*SUPPLIER~
IT1*1*100*EA*15.00**VP*SKU-001~
PID*F****Widget Model A~
IT1*2*50*EA*28.00**VP*SKU-002~
PID*F****Widget Model B~
TDS*290000~
CAD*M****PARCEL~
CTT*2~
SE*18*0001~
GE*1*2~
IEA*1*000000002~
```

---

## Multi-Adapter Scenario

Scenarios can contain multiple adapters of different types in one file. This is useful for end-to-end integration test setups.

### Example — Full order-to-cash scenario

One scenario stubbing an OData source, REST notification target, and SFTP outbound.

```json
{
  "version": 1,
  "name": "order-to-cash-full",
  "description": "Stubs all external systems involved in the order-to-cash CPI flow",
  "adapters": [
    {
      "name": "s4-salesorder-source",
      "type": "odata",
      "description": "S/4HANA OData source for new sales orders",
      "auth": {
        "type": "oauth2",
        "config": {
          "token_endpoint": "/oauth/token",
          "client_id": "cpi-s4-client",
          "client_secret": "s4oauth2secret",
          "token_ttl_seconds": 3600
        }
      },
      "config": {
        "odata_version": "v2",
        "service_path": "/sap/opu/odata/sap/API_SALES_ORDER_SRV",
        "namespace": "API_SALES_ORDER_SRV",
        "allow_write": false,
        "response_delay_ms": 60,
        "entity_sets": [
          {
            "name": "A_SalesOrder",
            "entity_type": "A_SalesOrderType",
            "key_property": "SalesOrder",
            "response_body": "{\"d\":{\"results\":[{\"SalesOrder\":\"1000000001\",\"SoldToParty\":\"1000001\",\"TotalNetAmount\":\"15000.00\",\"TransactionCurrency\":\"EUR\"}]}}"
          }
        ]
      }
    },
    {
      "name": "crm-notification-rest",
      "type": "rest",
      "description": "CRM system REST endpoint receiving order status notifications",
      "auth": {
        "type": "api_key",
        "config": {
          "header": "X-CRM-ApiKey",
          "value": "crm-key-abc123"
        }
      },
      "config": {
        "status_code": 200,
        "response_headers": {
          "Content-Type": "application/json"
        },
        "response_body": "{\"status\":\"RECEIVED\",\"message\":\"Notification accepted\"}",
        "response_delay_ms": 40
      }
    },
    {
      "name": "warehouse-sftp-outbound",
      "type": "sftp",
      "description": "Warehouse SFTP drop zone for picking instructions",
      "auth": {
        "type": "password",
        "config": {
          "username": "wh_cpi",
          "password": "Wh$ftp2024"
        }
      },
      "config": {
        "port": 22,
        "root_dir": "/picking",
        "files": []
      }
    }
  ]
}
```

---

## Design Notes

### What Is Not in the Scenario File

The following are **runtime-only** and are never stored in scenario JSON:

- `id` — assigned by the control plane at import time
- `status` — reflects live Kubernetes deployment state
- `ingress_url` — generated after the Kubernetes resources are created
- `created_at` / `updated_at` — managed by the control plane

This makes scenario files clean, portable, and safely importable into any environment without conflict.

### Version Field

The `version` integer allows the loader to detect older files and apply migrations. The current version is `1`. When the schema changes in a breaking way, version increments and a migration path is documented. Non-breaking additions (new optional fields) do not require a version bump.

### Auth Extensibility

New auth types (e.g., `mtls`, `saml`) can be added by defining a new `type` string and a `config` shape. Existing scenario files with known auth types are unaffected.

### Adapter Extensibility

New adapter types (e.g., `mqtt`, `amqp`) fit the same envelope — add a `type` string and document the `config` shape. The `name`, `auth`, and `description` fields are universal.

### EDI Formats (EDIFACT, ANSI X12)

EDIFACT and ANSI X12 are wire formats, not transport protocols. They are always delivered over a transport (SFTP or AS2). Rather than creating stub adapter types for each EDI format, the spec models the transport. The EDI content appears either as a pre-populated file in an SFTP adapter or as the payload delivered to an AS2 adapter.
