"""Strict local DayTrace bundle conversion; no network or persistence side effects."""
import base64
from datetime import datetime, timezone
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import re
import uuid
import zipfile

import av
import numpy as np

from .protocol import identity, uid, job, encode

NAMESPACE = uuid.UUID("827d3598-894e-5a92-b95b-70e9d362421e")
MAX_ARCHIVE_BYTES = 256*1024*1024
MAX_SOURCE_BYTES = 4*1024*1024
MAX_SOURCE_MS = 30000


def _integer(value,minimum,maximum,label):
    if type(value) is not int or not minimum <= value <= maximum:
        raise ValueError("invalid DayTrace "+label)
    return value


def _utc(milliseconds):
    return datetime.fromtimestamp(milliseconds/1000,timezone.utc).isoformat(timespec="milliseconds").replace("+00:00","Z")


def _decode_ogg(data,samples):
    chunks = []
    with av.open(io.BytesIO(data),mode="r") as container:
        if container.format.name != "ogg" or len(container.streams.audio)!=1:
            raise ValueError("source must contain exactly one Ogg Opus audio stream")
        stream = container.streams.audio[0]
        if stream.codec_context.name != "opus" or len(stream.codec_context.layout.channels)!=1:
            raise ValueError("source must be mono Opus")
        # Demux/decode applies OpusHead preskip and EOS discard-padding side data.
        resampler = av.AudioResampler(format="s16",layout="mono",rate=16000)
        decoded = 0
        for frame in container.decode(stream):
            for output in resampler.resample(frame):
                decoded += output.samples
                if decoded>samples+320:
                    raise ValueError("decoded source exceeds declared sample count")
                chunks.append(output.to_ndarray().reshape(-1))
        for output in resampler.resample(None):
            chunks.append(output.to_ndarray().reshape(-1))
    pcm = np.concatenate(chunks).astype(np.int16) if chunks else np.empty(0,dtype=np.int16)
    if len(pcm)!=samples:
        raise ValueError("decoded source sample count differs from metadata")
    return pcm


def _encode_chunk(pcm):
    encoder = av.CodecContext.create("libopus","w")
    encoder.sample_rate = 16000
    encoder.layout = "mono"
    encoder.format = "s16"
    encoder.bit_rate = 24000
    encoder.options = {"application":"voip","frame_duration":"20"}
    packets = []
    input_padding = (-len(pcm))%320
    padded = np.pad(pcm,(0,input_padding))
    for offset in range(0,len(padded),320):
        frame = av.AudioFrame.from_ndarray(padded[offset:offset+320].reshape(1,-1),format="s16",layout="mono")
        frame.sample_rate = 16000
        packets.extend(encoder.encode(frame))
    packets.extend(encoder.encode(None))
    # OpusHead preskip is stored in units of 48k samples, including for 16k input.
    extra = encoder.extradata
    if not extra or not extra.startswith(b"OpusHead") or len(extra)<12:
        raise ValueError("encoder did not expose Opus preskip")
    delay_48k = int.from_bytes(extra[10:12],"little")
    if delay_48k%3:
        raise ValueError("encoder delay cannot map to 16k samples")
    delay = delay_48k//3
    wire_samples = len(packets)*320
    if not 1<=len(packets)<=500 or wire_samples<len(pcm)+delay:
        raise ValueError("encoded segment cannot preserve full source tail")
    return [base64.b64encode(bytes(p)).decode("ascii") for p in packets],dict(
        encoder_delay_samples=delay,valid_offset_ms=delay/16,
        input_padding_samples=input_padding,tail_padding_samples=wire_samples-len(pcm)-delay)


def prepare_bundle(archive:Path,node_id:str):
    """Return (summary, [{segment, source}]); fail the whole bundle on any error."""
    identity(node_id)
    archive = Path(archive)
    if not 1<=archive.stat().st_size<=MAX_ARCHIVE_BYTES:
        raise ValueError("archive size exceeds limit")
    digest = hashlib.sha256()
    with archive.open("rb") as stream:
        for block in iter(lambda:stream.read(1024*1024),b""):
            digest.update(block)
    items = []
    with zipfile.ZipFile(archive) as bundle:
        entries = bundle.infolist()
        names = [entry.filename for entry in entries]
        if len(names)>20000 or len(set(names))!=len(names):
            raise ValueError("duplicate or excessive ZIP members")
        if sum(entry.file_size for entry in entries)>MAX_ARCHIVE_BYTES:
            raise ValueError("ZIP uncompressed size exceeds limit")
        for entry in entries:
            name = entry.filename
            if (len(name)>256 or "\\" in name or "\x00" in name or ":" in name
                    or PurePosixPath(name).is_absolute() or ".." in PurePosixPath(name).parts
                    or (entry.external_attr>>16)&0o170000==0o120000):
                raise ValueError("unsafe ZIP member path")
        manifest_info = bundle.getinfo("manifest.json")
        if manifest_info.file_size>8*1024*1024:
            raise ValueError("manifest exceeds size limit")
        def unique_fields(pairs):
            result = {}
            for key,value in pairs:
                if key in result:
                    raise ValueError("duplicate manifest JSON field")
                result[key] = value
            return result
        manifest = json.loads(bundle.read(manifest_info),object_pairs_hook=unique_fields)
        if not isinstance(manifest,dict) or manifest.get("schema")!="daytrace.analysis_bundle" or type(manifest.get("schema_version")) is not int or manifest["schema_version"]!=1:
            raise ValueError("unsupported DayTrace manifest")
        sources = manifest.get("segments")
        gaps = manifest.get("coverage_gaps",[])
        if not isinstance(sources,list) or not 1<=len(sources)<=10000 or not isinstance(gaps,list):
            raise ValueError("invalid source or coverage metadata")
        for gap in gaps:
            if not isinstance(gap,dict):
                raise ValueError("invalid coverage gap")
            start = _integer(gap.get("start_at_utc_ms"),0,253402300799000,"gap start")
            _integer(gap.get("end_at_utc_ms"),start,253402300799000,"gap end")
        seen_ids,seen_paths = set(),set()
        validated = []
        for source in sources:
            if not isinstance(source,dict):
                raise ValueError("invalid source metadata")
            uid(source.get("segment_id")); uid(source.get("session_id"))
            path = source.get("audio_path")
            checksum = source.get("sha256")
            if not isinstance(path,str) or not re.fullmatch(r"audio/[0-9]{6}\.opus",path):
                raise ValueError("invalid source audio path")
            if not isinstance(checksum,str) or not re.fullmatch(r"[0-9a-f]{64}",checksum):
                raise ValueError("invalid source checksum")
            if source["segment_id"] in seen_ids or path in seen_paths:
                raise ValueError("duplicate source identity or path")
            seen_ids.add(source["segment_id"]);seen_paths.add(path)
            start = _integer(source.get("start_at_utc_ms"),0,253402300799000,"start")
            duration = _integer(source.get("duration_ms"),20,MAX_SOURCE_MS,"duration")
            if duration%20 or source.get("input_sample_count")!=duration*16 or type(source.get("input_sample_count")) is not int:
                raise ValueError("source duration/sample count mismatch")
            if (type(source.get("end_at_utc_ms")) is not int or source["end_at_utc_ms"]!=start+duration
                    or type(source.get("input_sample_rate_hz")) is not int or source["input_sample_rate_hz"]!=16000
                    or type(source.get("channels")) is not int or source["channels"]!=1 or source.get("codec")!="opus"):
                raise ValueError("invalid source format or timeline")
            if source.get("start_at_utc") is not None and source["start_at_utc"]!=_utc(start):
                raise ValueError("source UTC timestamp mismatch")
            validated.append(source)
        validated.sort(key=lambda s:(s["start_at_utc_ms"],s["segment_id"]))
        for source in validated:
            info = bundle.getinfo(source["audio_path"])
            if not 1<=info.file_size<=MAX_SOURCE_BYTES:
                raise ValueError("source file size exceeds limit")
            data = bundle.read(info)
            if hashlib.sha256(data).hexdigest()!=source["sha256"]:
                raise ValueError("source checksum mismatch")
            pcm = _decode_ogg(data,source["input_sample_count"])
            frames = len(pcm)//320
            chunk_count = (frames+498)//499
            per_chunk,remainder = divmod(frames,chunk_count)
            offset = 0
            for chunk_index in range(chunk_count):
                valid_frames = per_chunk+(chunk_index<remainder)
                chunk = pcm[offset:offset+valid_frames*320]
                packets,padding = _encode_chunk(chunk)
                segment_id = str(uuid.uuid5(NAMESPACE,json.dumps([node_id,source["segment_id"],source["sha256"],chunk_index],separators=(",",":"))))
                # Stable across subset/incremental exports; timestamp controls
                # chronology. Reserve two low bits for this source's <=4 chunks.
                segment = dict(schema_version=1,segment_id=segment_id,session_id=source["session_id"],node_id=node_id,
                    sequence=(uuid.UUID(source["segment_id"]).int & ((1<<61)-1))*4+chunk_index,started_at=_utc(source["start_at_utc_ms"]+offset//16),duration_ms=len(packets)*20,
                    speech_ms=valid_frames*20,sample_rate=16000,channels=1,codec="opus",frame_ms=20,packets=packets)
                job(encode(dict(schema_version=1,job_id=segment_id,segment=segment)))
                ledger = dict(segment_id=source["segment_id"],session_id=source["session_id"],audio_path=source["audio_path"],sha256=source["sha256"],
                    offset_ms=offset//16,source_duration_ms=source["duration_ms"],valid_duration_ms=valid_frames*20,**padding)
                items.append(dict(segment=segment,source=ledger))
                offset += len(chunk)
            if offset!=len(pcm):
                raise ValueError("source splitting lost samples")
    source_duration = sum(source["duration_ms"] for source in validated)
    wire_duration = sum(item["segment"]["duration_ms"] for item in items)
    start = min(source["start_at_utc_ms"] for source in validated)
    end = max(source["end_at_utc_ms"] for source in validated)
    summary = dict(bundle_sha256=digest.hexdigest(),source_count=len(validated),chunk_count=len(items),source_duration_ms=source_duration,
        wire_duration_ms=wire_duration,padding_duration_ms=wire_duration-source_duration,sample_rate=16000,channels=1,
        start_at_utc_ms=start,end_at_utc_ms=end,started_at=_utc(start),ended_at=_utc(end),gap_count=len(gaps),
        timing_note="Wire duration includes Opus encoder delay and tail padding; source offsets and valid durations preserve original UTC. No word-level alignment is implied.",
        coverage_note="Coverage gaps mean audio was not exported; do not infer silence or absence of events.")
    return summary,items
